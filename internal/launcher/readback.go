package launcher

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// probePrefix marks the synthetic events this installer sends. Everything else in the hook table is
// a real session.
const probePrefix = "cardo-probe-"

// probeMarker is unique per run, so the read-back counts its own events and nothing else. It ends up
// in the service name (OTLP) and the session id (hook), both of which the collector keeps.
func probeMarker() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return probePrefix + hex.EncodeToString(b), nil
}

// clickhouseCount runs one query with the credentials the stack was generated with and returns the
// single number it asked for. The installer already owns .env; it does not need a second secret.
func clickhouseCount(ctx context.Context, opts Options, sql string) (int, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		return 0, fmt.Errorf("read .env: %w", err)
	}
	return chCount(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"], sql)
}

// postJSON sends a body to a loopback service and treats a non-2xx answer as a failure with the
// response attached: "accepted" must mean accepted.
func postJSON(ctx context.Context, target string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var out bytes.Buffer
		_, _ = out.ReadFrom(resp.Body)
		return fmt.Errorf("%s answered %s: %s", target, resp.Status, firstLine(out.String()))
	}
	return nil
}

func sendProbeLog(ctx context.Context, opts Options, marker string) error {
	payload := map[string]any{
		"resourceLogs": []any{map[string]any{
			"resource": map[string]any{"attributes": []any{
				map[string]any{"key": "service.name", "value": map[string]any{"stringValue": marker}},
			}},
			"scopeLogs": []any{map[string]any{
				"scope": map[string]any{"name": "cardo-installer"},
				"logRecords": []any{map[string]any{
					"timeUnixNano": strconv.FormatInt(time.Now().UnixNano(), 10),
					"severityText": "INFO",
					"body":         map[string]any{"stringValue": "installer read-back probe"},
				}},
			}},
		}},
	}
	return postJSON(ctx, opts.url("4319", "/v1/logs"), payload)
}

func sendProbeHook(ctx context.Context, opts Options, marker string) error {
	payload := map[string]any{
		"event":      "SessionEnd",
		"session_id": marker,
		"ts":         time.Now().UTC().Format(time.RFC3339),
	}
	return postJSON(ctx, opts.url("8089", "/v1/hooks"), payload)
}

// stepData is the step that turns "the ports answer" into "a row arrived". It sends one OTLP log and
// one hook event, then reads the counts back from ClickHouse. The exporter flushes on a 10 s timer,
// so the check polls instead of querying once.
func stepData(ctx context.Context, opts Options) (Result, error) {
	marker, err := probeMarker()
	if err != nil {
		return Result{ID: "data", Name: "DATA", Value: "probe not created", Cause: err.Error(),
			Fix: "Run the step again; the system random source did not answer."}, nil
	}

	if err := sendProbeLog(ctx, opts, marker); err != nil {
		return Result{
			ID: "data", Name: "DATA", Value: "OTLP probe rejected",
			Cause: "The collector refused or did not answer the OTLP probe: " + err.Error(),
			Fix:   "Check that the collector container is running and that CARDO_SALT in deploy/compose/.env is at least 32 hexadecimal characters; without it every batch is refused.",
		}, nil
	}
	if err := sendProbeHook(ctx, opts, marker); err != nil {
		return Result{
			ID: "data", Name: "DATA", Value: "hook probe rejected",
			Cause: "The hook endpoint refused or did not answer: " + err.Error(),
			Fix:   "Check that port 8089 is published by the collector (step 02) and that the container is running.",
		}, nil
	}

	logSQL := fmt.Sprintf("SELECT count() FROM cardo.bronze_otel_logs WHERE ServiceName = '%s'", marker)
	hookSQL := fmt.Sprintf("SELECT count() FROM cardo.bronze_hook_events WHERE LogAttributes['session_id'] = '%s'", marker)

	deadline := time.Now().Add(opts.pollTimeout())
	var logs, hooks int
	var lastErr error
	for {
		logs, lastErr = clickhouseCount(ctx, opts, logSQL)
		if lastErr == nil {
			hooks, lastErr = clickhouseCount(ctx, opts, hookSQL)
		}
		if lastErr == nil && logs > 0 && hooks > 0 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	if lastErr != nil {
		return Result{
			ID: "data", Name: "DATA", Value: "cannot read back",
			Cause: "The probe was accepted, but ClickHouse could not be queried: " + lastErr.Error(),
			Fix:   "Check that ClickHouse answers on 8124 with the credentials in deploy/compose/.env, then run this step again.",
		}, nil
	}
	if logs == 0 || hooks == 0 {
		return Result{
			ID: "data", Name: "DATA", Value: fmt.Sprintf("logs %d, hooks %d", logs, hooks),
			Cause: "Both probes were accepted, but nothing appeared in ClickHouse within the read-back window (logs " + strconv.Itoa(logs) + ", hooks " + strconv.Itoa(hooks) + ").",
			Fix:   "The exporter flushes on a 10 s timer; run this step again before changing anything. If it still fails, check the collector is running and that CARDO_SALT is valid.",
		}, nil
	}

	// The probe has done its job. Its rows are deleted so the session model never counts a synthetic
	// event as a person working: silver_session groups bronze by session_id with no filter of its own.
	cleanup := "probe rows removed"
	if err := deleteProbe(ctx, opts, marker); err != nil {
		cleanup = "probe rows kept: " + err.Error()
	}
	return Result{
		ID: "data", Name: "DATA", Value: fmt.Sprintf("read back: logs %d, hooks %d · %s", logs, hooks, cleanup), OK: true,
	}, nil
}

// deleteProbe removes the two synthetic rows by their marker. The tables have no key, so the marker
// is the only handle; a mutation is synchronous so the next read cannot see a half-deleted probe.
func deleteProbe(ctx context.Context, opts Options, marker string) error {
	queries := []string{
		"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE ServiceName = '" + marker + "' SETTINGS mutations_sync = 1",
		"ALTER TABLE cardo.bronze_hook_events DELETE WHERE LogAttributes['session_id'] = '" + marker + "' SETTINGS mutations_sync = 1",
	}
	for _, q := range queries {
		if err := clickhouseExec(ctx, opts, q); err != nil {
			return err
		}
	}
	return nil
}

// clickhouseExec runs a statement and ignores the body: ALTER DELETE answers 200 with no rows.
func clickhouseExec(ctx context.Context, opts Options, sql string) error {
	_, err := clickhouseCount(ctx, opts, sql)
	if err == nil {
		return nil
	}
	// A successful ALTER DELETE returns an empty body, which clickhouseCount cannot parse as a
	// number; that is success, not a parse failure.
	if strings.Contains(err.Error(), "returned \"\"") {
		return nil
	}
	return err
}

// stepFields is the read-only proof the views ask for: one real session, and the fields silver and
// gold actually read. It is a check, not an install step, and it waits for a real session.
func stepFields(ctx context.Context, opts Options) (Result, error) {
	row, err := latestSession(ctx, opts)
	if err != nil {
		return Result{
			ID: "fields", Name: "FIELDS", Value: "cannot read back",
			Cause: "ClickHouse could not be queried: " + err.Error(),
			Fix:   "Check that ClickHouse answers on 8124 with the credentials in deploy/compose/.env.",
		}, nil
	}
	if row == nil {
		return Result{
			ID: "fields", Name: "FIELDS", Value: "no real session yet", Pending: true,
			Fix: "Use Claude Code once in a project with the settings file installed, then run cardo check again.",
		}, nil
	}
	missing := []string{}
	if row.Pseudonym == "" {
		missing = append(missing, "pseudonym")
	}
	if row.AppVersion == "" {
		missing = append(missing, "app_version")
	}
	if !row.HasOTel {
		missing = append(missing, "otel rows")
	}
	if !row.HasHooks {
		missing = append(missing, "hook rows")
	}
	if row.Prompts == 0 {
		missing = append(missing, "prompt_id")
	}
	if len(missing) > 0 {
		return Result{
			ID: "fields", Name: "FIELDS", Value: fmt.Sprintf("%s · missing %s", shortID(row.ID), strings.Join(missing, ", ")),
			Cause: "The latest session is missing fields the views read: " + strings.Join(missing, ", ") + ".",
			Fix:   "Confirm the settings file is installed in that project and that the collector is running; then start a new session.",
		}, nil
	}
	return Result{ID: "fields", Name: "FIELDS", Value: shortID(row.ID) + " · pseudonym, version, otel, hooks, prompts", OK: true}, nil
}

// sessionRow is the slice of silver_session the check needs.
type sessionRow struct {
	ID         string
	Pseudonym  string
	AppVersion string
	HasOTel    bool
	HasHooks   bool
	Prompts    int
}

// latestSession reads the most recent session that the installer did not create. Probe markers are
// excluded twice: the probe deletes its rows, and the query ignores any that survive.
//
// It reads the _live views, which work silver out from the raw tables as they are read: the stored
// tables are rebuilt every 10 minutes (ADR-0052), and a session started a minute ago is the one
// this check is about.
func latestSession(ctx context.Context, opts Options) (*sessionRow, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	// Imported history and the seeded demo identity are not evidence about the live path: transcripts
	// carry no hook rows or app_version, and demo rows are synthetic. Only a real session can pass.
	sql := "SELECT session_id AS id, ifNull(pseudonym, '') AS pseudonym, app_version, has_otel, has_hooks, " +
		"(SELECT count() FROM cardo.silver_turn_live t WHERE t.session_id = s.session_id AND t.prompt_id != '') AS prompts " +
		"FROM cardo.silver_session_live s WHERE session_id NOT LIKE '" + probePrefix + "%' " +
		"AND session_id NOT LIKE 'demo-%' AND cohort != 'imported' " +
		"ORDER BY last_event_at DESC LIMIT 1 FORMAT JSONEachRow"
	body, _, err := chQuery(ctx, opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"], sql)
	if err != nil {
		return nil, err
	}
	raw := bytes.NewBufferString(body)
	if strings.TrimSpace(raw.String()) == "" {
		return nil, nil // ClickHouse answers 200 with an empty body for JSONEachRow and no rows
	}
	// JSONEachRow: one object per line, not an array.
	type row struct {
		ID         string `json:"id"`
		Pseudonym  string `json:"pseudonym"`
		AppVersion string `json:"app_version"`
		HasOTel    uint8  `json:"has_otel"`
		HasHooks   uint8  `json:"has_hooks"`
		Prompts    int    `json:"prompts"`
	}
	var r row
	found := false
	sc := bufio.NewScanner(strings.NewReader(raw.String()))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		found = true
		break // LIMIT 1
	}
	if !found {
		return nil, nil
	}
	return &sessionRow{ID: r.ID, Pseudonym: r.Pseudonym, AppVersion: r.AppVersion, HasOTel: r.HasOTel == 1, HasHooks: r.HasHooks == 1, Prompts: r.Prompts}, nil
}

// shortID keeps a session id recognisable without pasting 36 characters into a table.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// stepSession looks for a hook event that the installer did not send. It is the row the engineer will
// recognise, and it can only appear after their first real Claude Code session.
func stepSession(ctx context.Context, opts Options) (Result, error) {
	sql := "SELECT count() FROM cardo.bronze_hook_events WHERE LogAttributes['session_id'] NOT LIKE '" + probePrefix + "%' AND LogAttributes['session_id'] != ''"
	n, err := clickhouseCount(ctx, opts, sql)
	if err != nil {
		return Result{
			ID: "session", Name: "SESSION", Value: "cannot read back",
			Cause: "ClickHouse could not be queried: " + err.Error(),
			Fix:   "Check that ClickHouse answers on 8124 with the credentials in deploy/compose/.env.",
		}, nil
	}
	if n == 0 {
		return Result{
			ID: "session", Name: "SESSION", Value: "no session yet", Pending: true,
			Fix: "Run claude once in a project with the settings file installed, then check again. This is the only step that needs you to work normally.",
		}, nil
	}
	return Result{ID: "session", Name: "SESSION", Value: fmt.Sprintf("hook events read back: %d", n), OK: true}, nil
}
