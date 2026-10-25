package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/retention"
)

// overviewView is the single answer the landing screen needs: what runs, what is fresh, what to do.
type overviewView struct {
	Stack    factView    `json:"stack"`
	Poller   adminView   `json:"poller"`
	Sessions factView    `json:"sessions"`
	Warnings []string    `json:"warnings"`
	Next     actionView  `json:"next"`
	AsOf     string      `json:"asOf"`
	Changes  []changeRow `json:"changes"`
	Session  *sessionCard
}

type factView struct {
	OK    bool   `json:"ok"`
	Value string `json:"value"`
	Cause string `json:"cause,omitempty"`
	Fix   string `json:"fix,omitempty"`
}

type actionView struct {
	Surface string `json:"surface"` // install | settings | overview
	Title   string `json:"title"`
	Detail  string `json:"detail"`
}

// sessionCard is the operator's own proof: the newest session on this stack. It carries no identity
// beyond the pseudonym the collectors already use.
type sessionCard struct {
	ID         string   `json:"id"`
	AppVersion string   `json:"appVersion"`
	StartedAt  string   `json:"startedAt"`
	LastEvent  string   `json:"lastEventAt"`
	Ended      bool     `json:"ended"`
	EndReason  string   `json:"endReason,omitempty"`
	HasOTel    bool     `json:"hasOtel"`
	HasHooks   bool     `json:"hasHooks"`
	Prompts    int      `json:"prompts"`
	Artifacts  []string `json:"artifacts"`
}

func (s *Server) ch(ctx context.Context, sql string) (string, error) {
	env, err := ReadDotEnv(filepath.Join(s.opts.Dir, ".env"))
	if err != nil {
		return "", fmt.Errorf("read .env: %w", err)
	}
	body, _, err := chQuery(ctx, s.opts.url("8124", ""), env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"], sql)
	return body, err
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	fresh := r.URL.Query().Get("fresh") != ""
	if !fresh {
		s.ovMu.Lock()
		if s.ovBody != nil && time.Since(s.ovAt) < 3*time.Second {
			body := s.ovBody
			s.ovMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		s.ovMu.Unlock()
	}
	ctx := r.Context()
	out := overviewView{}

	rows, err := Check(ctx, s.opts)
	if err != nil {
		out.Stack = factView{Cause: err.Error(), Fix: "Start the stack: cardo up"}
	} else {
		var failing []string
		for _, row := range rows {
			if !row.OK && !row.Pending {
				failing = append(failing, row.Name+": "+row.Value)
				if out.Stack.Cause == "" {
					out.Stack.Cause, out.Stack.Fix = row.Cause, row.Fix
				}
			}
		}
		out.Stack.OK = len(failing) == 0
		if out.Stack.OK {
			passed := 0
			pending := 0
			for _, row := range rows {
				if row.OK {
					passed++
				}
				if row.Pending {
					pending++
					out.Warnings = append(out.Warnings, row.Name+": "+row.Value)
				}
			}
			if pending > 0 {
				out.Stack.Value = fmt.Sprintf("%d/%d · %d waiting", passed, len(rows), pending)
			} else {
				out.Stack.Value = fmt.Sprintf("%d checks pass", passed)
			}
		} else {
			out.Stack.Value = failing[0]
			out.Warnings = append(out.Warnings, failing...)
		}
	}

	out.Poller = s.adminState(ctx)

	last, count, err := s.sessionFacts(ctx)
	if err != nil {
		out.Sessions = factView{Cause: err.Error(), Fix: "Check that ClickHouse answers on 8124."}
	} else if count == 0 {
		out.Sessions = factView{Value: "no session yet", Fix: "Run Claude Code once in a project with the settings file, then press Check."}
	} else {
		out.Sessions = factView{OK: true, Value: fmt.Sprintf("%d sessions in 7 days · last %s", count, last)}
	}

	if card, err := s.latestSession(ctx); err == nil {
		out.Session = card
	}

	// One action, chosen by what blocks the operator's sentence the most.
	hasDir, approved := s.consolePreconditions(ctx)
	switch {
	case !out.Stack.OK:
		out.Next = actionView{Surface: "install", Title: "Finish the installation", Detail: firstNonEmpty(out.Stack.Cause, "A check is failing.")}
	case count == 0:
		out.Next = actionView{Surface: "install", Title: "Prove it with your own session", Detail: "Run Claude Code once in an instrumented project, then check the session step."}
	case !hasDir:
		out.Next = actionView{Surface: "teams", Title: "Load the team directory", Detail: "Without it no session can be attributed to a team, and every team view stays empty."}
	case approved == 0:
		out.Next = actionView{Surface: "catalog", Title: "Approve the first artifact", Detail: "Drafts change nothing the views read; only an approved version becomes the organization's standard."}
	case !out.Poller.Configured:
		out.Next = actionView{Surface: "settings", Title: "Connect the Admin API (optional)", Detail: "Without a key the poller stays off; the collector path still works."}
	default:
		out.Next = actionView{Surface: "overview", Title: "Nothing needs attention", Detail: "Both ingestion paths are answering, the directory is loaded and an artifact is approved."}
	}
	out.AsOf = time.Now().Format("15:04:05")
	out.Changes, _ = s.recentChanges(ctx, 5)
	body, err := json.Marshal(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.ovMu.Lock()
	s.ovBody, s.ovAt = body, time.Now()
	s.ovMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// sessionFacts answers "how fresh is the collector path" in one query pair.
func (s *Server) sessionFacts(ctx context.Context) (string, int, error) {
	body, err := s.ch(ctx, "SELECT count() AS n, toString(max(last_event_at)) AS last FROM cardo.silver_session WHERE session_id NOT LIKE '"+probePrefix+"%' AND last_event_at > now() - INTERVAL 7 DAY FORMAT JSONEachRow")
	if err != nil {
		return "", 0, err
	}
	var row struct {
		N    int    `json:"n"`
		Last string `json:"last"`
	}
	if err := json.Unmarshal([]byte(firstLine(body)), &row); err != nil {
		return "", 0, err
	}
	return shortTime(row.Last), row.N, nil
}

func (s *Server) latestSession(ctx context.Context) (*sessionCard, error) {
	sql := "SELECT session_id, app_version, toString(started_at) AS started, toString(last_event_at) AS last, ended, end_reason, has_otel, has_hooks, " +
		"(SELECT count() FROM cardo.silver_turn t WHERE t.session_id = s.session_id) AS prompts " +
		"FROM cardo.silver_session s WHERE session_id NOT LIKE '" + probePrefix + "%' ORDER BY last_event_at DESC LIMIT 1 FORMAT JSONEachRow"
	body, err := s.ch(ctx, sql)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	var row struct {
		ID        string `json:"session_id"`
		App       string `json:"app_version"`
		Started   string `json:"started"`
		Last      string `json:"last"`
		Ended     uint8  `json:"ended"`
		EndReason string `json:"end_reason"`
		HasOTel   uint8  `json:"has_otel"`
		HasHooks  uint8  `json:"has_hooks"`
		Prompts   int    `json:"prompts"`
	}
	if err := json.Unmarshal([]byte(firstLine(body)), &row); err != nil {
		return nil, err
	}
	card := &sessionCard{
		ID: row.ID, AppVersion: row.App, StartedAt: shortTime(row.Started), LastEvent: shortTime(row.Last),
		Ended: row.Ended == 1, EndReason: row.EndReason, HasOTel: row.HasOTel == 1, HasHooks: row.HasHooks == 1, Prompts: row.Prompts,
	}
	arts, err := s.ch(ctx, "SELECT name, kind FROM cardo.silver_artifact_load WHERE session_id = '"+row.ID+"' GROUP BY name, kind ORDER BY name FORMAT JSONEachRow")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(arts), "\n") {
			var a struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			}
			if json.Unmarshal([]byte(line), &a) == nil && a.Name != "" {
				card.Artifacts = append(card.Artifacts, a.Kind+" "+a.Name)
			}
		}
	}
	return card, nil
}

// settingsView is what the Settings surface may read and write.
type settingsView struct {
	MinGroupSize  int    `json:"minGroupSize"`
	PilotMode     bool   `json:"pilotMode"`
	OrgArtifacts  string `json:"orgArtifacts"`
	RetentionDays int    `json:"retentionDays"`
	// RetentionDefault is true when the organization has chosen no window, so the effective one is
	// the default (ADR-0053). The console says so rather than showing a number with no history.
	RetentionDefault bool      `json:"retentionDefault"`
	RetentionNote    string    `json:"retentionNote"`
	Ports            []int     `json:"ports"`
	Dir              string    `json:"dir"`
	Poller           adminView `json:"poller"`
	Version          string    `json:"version"`
	Error            string    `json:"error,omitempty"`
}

// retentionNote explains the window in one sentence, and says what a longer one means. It is shown
// next to the value, because the number is a privacy promise and not a housekeeping setting.
func retentionNote(days int, isDefault bool) string {
	note := "90 days is long enough to compare a few weeks before and after a tool is rolled out, and short enough that rows tied to a code name don't linger. The grouped totals, which carry no code name, are kept anyway, so long-term trends don't need longer raw data."
	switch {
	case isDefault:
		return fmt.Sprintf("%d (default). %s", retention.DefaultDays, note)
	case days == 0:
		return "0: nothing is deleted and the tables' TTL is taken off, so nothing expires on its own either. This is a choice to keep the rows, and it is the one that keeps personal data longest."
	case days > retention.DefaultDays:
		return fmt.Sprintf("%d days, above the default of %d. Rows tied to a code name are kept longer than the default; tell engineers.", days, retention.DefaultDays)
	default:
		return fmt.Sprintf("%d days. %s", days, note)
	}
}

func (s *Server) settingsView(ctx context.Context) settingsView {
	v := settingsView{MinGroupSize: 5, Ports: PublishPorts, Dir: s.opts.Dir, Version: s.opts.Version, Poller: s.adminState(ctx)}
	body, err := s.ch(ctx, "SELECT min_group_size, org_artifacts FROM cardo.settings_effective LIMIT 1 FORMAT JSONEachRow")
	if err != nil {
		v.Error = err.Error()
		return v
	}
	if strings.TrimSpace(body) != "" {
		var row struct {
			Min int    `json:"min_group_size"`
			Org string `json:"org_artifacts"`
		}
		if json.Unmarshal([]byte(firstLine(body)), &row) == nil {
			v.MinGroupSize, v.OrgArtifacts = row.Min, row.Org
		}
	}
	if body, err := s.ch(ctx, "SELECT argMax(value, version) FROM cardo.settings WHERE key = 'pilot_mode' FORMAT TSV"); err == nil {
		v.PilotMode = strings.TrimSpace(body) == "1"
	}
	if body, err := s.ch(ctx, "SELECT argMax(value, version) FROM cardo.settings WHERE key = 'retention_days' FORMAT TSV"); err == nil {
		raw := strings.TrimSpace(body)
		if raw == "" {
			// Nothing was chosen: the default window is in force (ADR-0053).
			v.RetentionDays = retention.DefaultDays
			v.RetentionDefault = true
		} else if n, err := strconv.Atoi(raw); err == nil {
			v.RetentionDays = n
			v.RetentionDefault = false
		}
	}
	v.RetentionNote = retentionNote(v.RetentionDays, v.RetentionDefault)
	return v
}

func (s *Server) handleLatestSession(w http.ResponseWriter, r *http.Request) {
	card, err := s.latestSession(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, card)
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.settingsView(r.Context()))
}

// handleSettingsPost writes only what changed, each key as a new versioned row. The views read the
// highest version, so there is no update-in-place race.
func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MinGroupSize  *int    `json:"minGroupSize"`
		OrgArtifacts  *string `json:"orgArtifacts"`
		PilotMode     *bool   `json:"pilotMode"`
		RetentionDays *int    `json:"retentionDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `want {"minGroupSize": 5, "orgArtifacts": "slug,slug"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if body.PilotMode != nil {
		value := "0"
		if *body.PilotMode {
			value = "1"
		}
		if _, err := s.ch(ctx, "INSERT INTO cardo.settings (key, value, version) SELECT 'pilot_mode', '"+value+"', max(version) + 1 FROM cardo.settings"); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := s.audit(ctx, "setting", "pilot_mode", 0, "", "", value); err != nil {
			http.Error(w, "the setting was written but its audit line failed: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	if body.MinGroupSize != nil {
		n := *body.MinGroupSize
		if n < 5 || n > 100 {
			http.Error(w, "minGroupSize must be between 5 and 100: below five a cohort is a person", http.StatusBadRequest)
			return
		}
		if _, err := s.ch(ctx, fmt.Sprintf("INSERT INTO cardo.settings (key, value, version) SELECT 'min_group_size', '%d', max(version) + 1 FROM cardo.settings", n)); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := s.audit(ctx, "setting", "min_group_size", 0, "", "", fmt.Sprint(n)); err != nil {
			http.Error(w, "the setting was written but its audit line failed: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	if body.OrgArtifacts != nil {
		list := strings.TrimSpace(*body.OrgArtifacts)
		if _, err := s.ch(ctx, "INSERT INTO cardo.settings (key, value, version) SELECT 'org_artifacts', '"+escapeSQL(list)+"', max(version) + 1 FROM cardo.settings"); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		// The collector reads this from .env; keep the two in step.
		env, err := ReadDotEnv(filepath.Join(s.opts.Dir, ".env"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		env["CARDO_ORG_ARTIFACTS"] = list
		if err := WriteDotEnv(filepath.Join(s.opts.Dir, ".env"), env, Secrets{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.RetentionDays != nil {
		n := *body.RetentionDays
		if n < 0 || (n > 0 && n < 7) || n > 3650 {
			http.Error(w, "retentionDays must be 0 (keep) or between 7 and 3650", http.StatusBadRequest)
			return
		}
		if _, err := s.ch(ctx, fmt.Sprintf("INSERT INTO cardo.settings (key, value, version) SELECT 'retention_days', '%d', max(version) + 1 FROM cardo.settings", n)); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		env, err := ReadDotEnv(filepath.Join(s.opts.Dir, ".env"))
		if err == nil {
			env["CARDO_RETENTION_DAYS"] = fmt.Sprint(n)
			if err := WriteDotEnv(filepath.Join(s.opts.Dir, ".env"), env, Secrets{}); err != nil {
				http.Error(w, "retention was saved but .env could not be updated: "+err.Error(), http.StatusBadGateway)
				return
			}
		}
		if err := s.audit(ctx, "setting", "retention_days", 0, "", "", fmt.Sprint(n)); err != nil {
			http.Error(w, "the setting was written but its audit line failed: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	writeJSON(w, s.settingsView(ctx))
}

// escapeSQL keeps a single quote in a slug from ending the statement. Values here are operator input,
// not identifiers.
func escapeSQL(v string) string { return strings.ReplaceAll(v, "'", "''") }

// shortTime trims ClickHouse's nanosecond timestamps to something a person reads.
func shortTime(v string) string {
	if len(v) >= 19 {
		return v[:19]
	}
	return v
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// parseInt is used by the settings form on the client; kept here for symmetry in tests.
func parseIntOr(v string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return n
	}
	return fallback
}

// changeRow is one audited write, as the console shows it.
type changeRow struct {
	At     string `json:"at"`
	Action string `json:"action"`
	Object string `json:"object"`
	Note   string `json:"note"`
}

func (s *Server) recentChanges(ctx context.Context, limit int) ([]changeRow, error) {
	body, err := s.ch(ctx, fmt.Sprintf("SELECT toString(at) AS at, action, object, note FROM cardo.settings_changes ORDER BY at DESC LIMIT %d FORMAT JSONEachRow", limit))
	if err != nil {
		return nil, err
	}
	var out []changeRow
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r changeRow
		if json.Unmarshal([]byte(line), &r) == nil {
			r.At = shortTime(r.At)
			out = append(out, r)
		}
	}
	return out, nil
}

// consolePreconditions answers whether a directory is loaded and whether anything is approved.
func (s *Server) consolePreconditions(ctx context.Context) (bool, int) {
	hasDir := false
	if n, err := s.chInt(ctx, "SELECT count() FROM cardo.settings_teams_batches"); err == nil && n > 0 {
		hasDir = true
	}
	approved := 0
	if n, err := s.chInt(ctx, "SELECT count() FROM cardo.settings_artifact_versions WHERE status = 'approved'"); err == nil {
		approved = n
	}
	return hasDir, approved
}
