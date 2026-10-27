// Package retention deletes raw rows older than the operator's window. Gold is views over silver,
// and silver is views or tables rebuilt from bronze, so only bronze is deleted from; a shorter
// window therefore shortens every downstream number at once (ADR-0012). The stored silver tables
// carry bronze's expiry as well, in case their rebuilds stop, and it moves with bronze's (ADR-0052).
package retention

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultDays is the fleet-core window when the organization has not chosen one (ADR-0053). It is
// what the shipped tables' TTL already does, and what the console shows as "90 (default)".
const DefaultDays = 90

// Table is one table retention ages, and how.
type Table struct {
	Name string
	Age  string // a SQL predicate over the table, e.g. "day < today() - 30"; empty when nothing is deleted from it
	TTL  string // the expression the table's TTL is built on, e.g. "day"; empty when it carries none
	// Refresher is the refreshable view that rebuilds a stored silver table, empty for any other.
	// It is stopped while the table's TTL moves (see alterTTL).
	Refresher string
}

// storedSilver lists the silver tables rebuilt from bronze every 10 minutes, each with the
// refresher that rebuilds it (sql/clickhouse/017_stored_silver.sql).
var storedSilver = [][2]string{
	{"cardo.silver_session", "cardo.silver_session_refresh"},
	{"cardo.silver_api_request", "cardo.silver_api_request_refresh"},
	{"cardo.silver_tool_call", "cardo.silver_tool_call_refresh"},
	{"cardo.silver_turn", "cardo.silver_turn_refresh"},
	{"cardo.silver_policy_decision", "cardo.silver_policy_decision_refresh"},
	{"cardo.silver_context_event", "cardo.silver_context_event_refresh"},
	{"cardo.silver_artifact_load_stored", "cardo.silver_artifact_load_refresh"},
}

// Tables is the fixed list retention owns.
//
// The staging table carries no TTL on purpose: its rows live for seconds, between the write and
// the partition swap (sql/clickhouse/001_bronze.sql). Retention still ages out what a crashed
// poll left behind, but it does not put a TTL on a table that is meant to have none.
//
// Nothing is deleted from the stored silver tables. Each rebuild starts again from bronze, so a row
// deleted there leaves silver at the next rebuild, within 10 minutes. Only their TTL moves.
func Tables(days int) []Table {
	tables := []Table{
		{Name: "cardo.bronze_actor_day", Age: fmt.Sprintf("day < today() - %d", days), TTL: "day"},
		{Name: "cardo.bronze_actor_day_staging", Age: fmt.Sprintf("day < today() - %d", days)},
		{Name: "cardo.bronze_otel_logs", Age: fmt.Sprintf("Timestamp < now() - INTERVAL %d DAY", days), TTL: "toDate(Timestamp)"},
		{Name: "cardo.bronze_hook_events", Age: fmt.Sprintf("Timestamp < now() - INTERVAL %d DAY", days), TTL: "toDate(Timestamp)"},
		{Name: "cardo.bronze_otel_metrics_sum", Age: fmt.Sprintf("TimeUnix < now() - INTERVAL %d DAY", days), TTL: "toDate(TimeUnix)"},
	}
	for _, s := range storedSilver {
		tables = append(tables, Table{Name: s[0], TTL: "day", Refresher: s[1]})
	}
	return tables
}

// Report says how many rows each table would delete, or did.
type Report map[string]int64

// Prune deletes rows older than days. With dryRun it only counts.
//
// Zero means "keep everything": no row is deleted, and the tables' TTL is removed, so nothing
// expires on its own either. That has to be said explicitly, because a TTL left in place would
// keep deleting at the shipped window whatever the operator chose.
func Prune(ctx context.Context, endpoint, user, pass string, days int, dryRun bool) (Report, error) {
	if days < 0 {
		return nil, fmt.Errorf("retention window must be zero or more days")
	}
	out := Report{}
	for _, t := range Tables(days) {
		if days == 0 {
			// Keep everything. Nothing is deleted, and the TTL comes off, so nothing expires on
			// its own either. A dry run says the same thing without touching the table.
			if !dryRun && t.TTL != "" {
				if err := alterTTL(ctx, endpoint, user, pass, t, "REMOVE TTL"); err != nil {
					return out, fmt.Errorf("%s: %w", t.Name, err)
				}
			}
			if t.Age != "" {
				out[t.Name] = 0
			}
			continue
		}
		if !dryRun && t.TTL != "" {
			// The table ships with a 90-day TTL; the operator's window wins, in both directions.
			if err := alterTTL(ctx, endpoint, user, pass, t, fmt.Sprintf("MODIFY TTL %s + toIntervalDay(%d)", t.TTL, days)); err != nil {
				return out, fmt.Errorf("%s: %w", t.Name, err)
			}
		}
		if t.Age == "" {
			continue
		}
		n, err := count(ctx, endpoint, user, pass, fmt.Sprintf("SELECT count() FROM %s WHERE %s", t.Name, t.Age))
		if err != nil {
			return out, fmt.Errorf("%s: %w", t.Name, err)
		}
		out[t.Name] = n
		if dryRun || n == 0 {
			continue
		}
		if err := exec(ctx, endpoint, user, pass, fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s SETTINGS mutations_sync=1", t.Name, t.Age)); err != nil {
			return out, fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	return out, nil
}

// ApplyWindow moves the tables' TTL to the window and deletes nothing.
//
// The TTL is what enforces the window: ClickHouse drops a row once its TTL expression passes, so
// moving the TTL is enough to make a shorter window take effect on its own. The deletion in Prune
// only makes the rows go now rather than at the next merge.
//
// This is what the console calls when the operator saves a window, so the number it shows is the
// one in force, rather than one that waits for the next poll to arrive.
func ApplyWindow(ctx context.Context, endpoint, user, pass string, days int) error {
	if days < 0 {
		return fmt.Errorf("retention window must be zero or more days")
	}
	for _, t := range Tables(days) {
		if t.TTL == "" {
			continue
		}
		if days == 0 {
			if err := alterTTL(ctx, endpoint, user, pass, t, "REMOVE TTL"); err != nil {
				return fmt.Errorf("%s: %w", t.Name, err)
			}
			continue
		}
		if err := alterTTL(ctx, endpoint, user, pass, t, fmt.Sprintf("MODIFY TTL %s + toIntervalDay(%d)", t.TTL, days)); err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	return nil
}

// alterTTL changes one table's TTL.
//
// A stored silver table's refresher is stopped first and started again after. A rebuild fills a
// new table made from the old one's definition as the rebuild began, and swaps it in when done,
// so a TTL changed while a rebuild runs comes back as it was. Measured on ClickHouse 26.6: a
// 21-day TTL set during a rebuild was 60 again once the rebuild finished. Stopping the refresher
// cancels a rebuild in progress, and the next one starts from the changed table.
func alterTTL(ctx context.Context, endpoint, user, pass string, t Table, change string) error {
	alter := fmt.Sprintf("ALTER TABLE %s %s", t.Name, change)
	if t.Refresher == "" {
		return exec(ctx, endpoint, user, pass, alter)
	}
	if err := exec(ctx, endpoint, user, pass, "SYSTEM STOP VIEW "+t.Refresher); err != nil {
		return err
	}
	err := exec(ctx, endpoint, user, pass, alter)
	if serr := exec(ctx, endpoint, user, pass, "SYSTEM START VIEW "+t.Refresher); err == nil {
		err = serr
	}
	return err
}

// EffectiveWindow returns the window in force: the one the operator chose in the console, or the
// fallback when nothing was chosen there.
//
// The console writes its choice to cardo.settings and shows that value, so the settings row is
// what everything else has to agree with. The environment is only the fallback for an installation
// whose console has never been opened.
func EffectiveWindow(ctx context.Context, endpoint, user, pass string, fallback int) (int, error) {
	if endpoint == "" {
		return fallback, fmt.Errorf("ClickHouse endpoint is not set")
	}
	body, err := query(ctx, endpoint, user, pass,
		"SELECT argMax(value, version) FROM cardo.settings WHERE key = 'retention_days' FORMAT TSV")
	if err != nil {
		return fallback, err
	}
	raw := strings.TrimSpace(body)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback, fmt.Errorf("the stored retention window %q is not a number", raw)
	}
	return n, nil
}

func count(ctx context.Context, endpoint, user, pass, sql string) (int64, error) {
	body, err := query(ctx, endpoint, user, pass, sql)
	if err != nil {
		return 0, err
	}
	var n int64
	_, err = fmt.Sscanf(strings.TrimSpace(body), "%d", &n)
	return n, err
}

func exec(ctx context.Context, endpoint, user, pass, sql string) error {
	_, err := query(ctx, endpoint, user, pass, sql)
	return err
}

func query(ctx context.Context, endpoint, user, pass, sql string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("ClickHouse endpoint is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", pass)
	req.Header.Set("Content-Type", "text/plain")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("clickhouse returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}
