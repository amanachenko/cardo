package clickhouse

// The event name a collector that started too early leaves out (sql/clickhouse/022).
//
// The collector's ClickHouse exporter reads each table's columns once, at start-up. If ClickHouse
// is not answering then, it writes every row without EventName for the rest of its life, and every
// view, which matches on EventName, misses them. These tests run against a real ClickHouse and are
// skipped unless CARDO_CLICKHOUSE_URL is set:
//
//	CARDO_CLICKHOUSE_URL=... CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
//	  go test ./internal/store/clickhouse/ -run EventName -count=1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	cardosql "github.com/amanachenko/cardo/sql"
)

// mapLiteral writes a ClickHouse map literal. The test values hold no quote.
func mapLiteral(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("'%s':'%s'", k, m[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// degradedInsert is the INSERT the exporter sends once its start-up read of the table has failed:
// every column it knows, but no EventName.
func degradedInsert(table string, at time.Time, attrs map[string]string) string {
	return fmt.Sprintf(
		"INSERT INTO %s (Timestamp, TraceId, SpanId, TraceFlags, SeverityText, SeverityNumber, ServiceName, "+
			"Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, "+
			"ScopeAttributes, LogAttributes) "+
			"VALUES ('%s', '', '', 0, '', 0, 'claude-code', '', '', {'cardo.tier':'1'}, '', '', '', {}, %s)",
		table, stamp(at), mapLiteral(attrs))
}

// eventNames reads one session's rows from a bronze table as "name=count", in name order, the
// empty name written as (none).
func eventNames(t *testing.T, st *Store, table, sessionKey, session string) string {
	t.Helper()
	out := trendQuery(t, st, fmt.Sprintf(
		"SELECT if(EventName = '', '(none)', EventName), count() FROM %s WHERE LogAttributes['%s'] = '%s' "+
			"GROUP BY EventName ORDER BY EventName FORMAT TabSeparated", table, sessionKey, session))
	return strings.Join(strings.Fields(strings.ReplaceAll(out, "\t", "=")), " ")
}

// A collector that started before ClickHouse answered leaves EventName out of every INSERT. The
// column's default gives the row its name from its own attributes, as the collector would have.
func TestAnInsertWithoutTheEventNameStillStoresIt(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	run := fmt.Sprintf("evname-%d", time.Now().UnixNano())
	at := time.Now().UTC().AddDate(0, 0, -14).Truncate(time.Hour)
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session.id'], '%s') SETTINGS mutations_sync = 1", logsTable, run))
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session_id'], '%s') SETTINGS mutations_sync = 1", hooksTable, run))
	})

	inserts := []string{
		degradedInsert(logsTable, at, map[string]string{"event.name": "user_prompt", "session.id": run + "-otel"}),
		degradedInsert(hooksTable, at, map[string]string{"hook_event_name": "UserPromptSubmit", "session_id": run + "-hook"}),
		// A row with no name anywhere stays without one, rather than becoming "claude_code.".
		degradedInsert(logsTable, at, map[string]string{"session.id": run + "-none"}),
		// A healthy exporter names the row itself, and that name is kept.
		fmt.Sprintf("INSERT INTO %s (Timestamp, LogAttributes, EventName) VALUES ('%s', %s, 'claude_code.api_request')",
			logsTable, stamp(at), mapLiteral(map[string]string{"event.name": "user_prompt", "session.id": run + "-named"})),
	}
	for _, q := range inserts {
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}

	for _, c := range []struct{ table, key, session, want string }{
		{logsTable, "session.id", run + "-otel", "claude_code.user_prompt=1"},
		{hooksTable, "session_id", run + "-hook", "UserPromptSubmit=1"},
		{logsTable, "session.id", run + "-none", "(none)=1"},
		{logsTable, "session.id", run + "-named", "claude_code.api_request=1"},
	} {
		if got := eventNames(t, st, c.table, c.key, c.session); got != c.want {
			t.Errorf("%s, session %s: stored %q, want %q", c.table, c.session, got, c.want)
		}
	}
}

// bronzeRow is one stored row with the EventName given, empty for one the exporter left out.
func bronzeRow(buf *bytes.Buffer, at time.Time, eventName string, attrs, resource map[string]string) {
	json.NewEncoder(buf).Encode(map[string]any{
		"Timestamp": stamp(at), "EventName": eventName, "LogAttributes": attrs, "ResourceAttributes": resource,
	})
}

// Rows already stored without their name are repaired once by the migration, and the trend rows
// they made wrong are cleared for `cardo trend` to recompute. The migration's statements are run
// twice, as the runner would after a failure partway: the second run must change nothing.
func TestTheRepairNamesTheStoredRowsOnce(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	run := fmt.Sprintf("evrepair-%d", time.Now().UnixNano())
	const marker = 987654321 // the trend rows this test writes, told apart from real ones

	now := time.Now().UTC()
	monday := now.Truncate(24*time.Hour).AddDate(0, 0, -int((now.Weekday()+6)%7))
	touched := monday.AddDate(0, 0, -14)  // inside the 30-day window set below
	untouched := touched.AddDate(0, 0, 1) // no nameless row on it
	tooOld := monday.AddDate(0, 0, -42)   // outside it: `cardo trend` would refuse to recompute it
	day := func(d time.Time) string { return d.Format("2006-01-02") }
	sessionA, sessionB := run+"-a", run+"-b"

	// Registered first, so it runs last: the rows go, then the touched day and its week are
	// recomputed, which on a developer's own stack puts back the real trend rows the repair cleared.
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session.id'], '%s') SETTINGS mutations_sync = 1", logsTable, run))
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session_id'], '%s') SETTINGS mutations_sync = 1", hooksTable, run))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.trend_day DELETE WHERE sessions = %d SETTINGS mutations_sync = 1", marker))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.trend_week_artifact DELETE WHERE uses = %d SETTINGS mutations_sync = 1", marker))
		if err := RebuildSilver(ctx, st.c); err != nil {
			t.Errorf("cannot rebuild silver after the clean-up: %v", err)
		}
		if err := st.Trend(ctx, day(touched)); err != nil {
			t.Errorf("cannot recompute %s in the clean-up: %v", day(touched), err)
		}
	})

	// A 30-day window, put back before the clean-up above recomputes.
	prev := trendQuery(t, st, "SELECT argMax(value, version) FROM cardo.settings WHERE key = 'retention_days' FORMAT TabSeparated")
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"INSERT INTO cardo.settings (key, value, version) SELECT 'retention_days', '%s', max(version) + 1 FROM cardo.settings", prev))
	})
	if err := st.c.Exec(ctx,
		"INSERT INTO cardo.settings (key, value, version) SELECT 'retention_days', '30', max(version) + 1 FROM cardo.settings"); err != nil {
		t.Fatal(err)
	}

	// Session A, on the touched day: a prompt, its request and its hook, all stored nameless.
	var logs, hooks bytes.Buffer
	at := touched.Add(10 * time.Hour)
	who := map[string]string{"cardo.tier": "1", "cardo.salt_version": "v1"}
	otelA := func(event string, kv ...string) map[string]string {
		m := map[string]string{"event.name": event, "session.id": sessionA, "prompt.id": run + "-q1",
			"user.pseudonym": "p-" + run, "app.version": "2.1.281"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	bronzeRow(&logs, at, "", otelA("user_prompt", "prompt_length", "40"), who)
	bronzeRow(&logs, at.Add(5*time.Second), "", otelA("api_request", "query_source", "repl_main_thread",
		"model", "claude-sonnet-4-6", "input_tokens", "100", "output_tokens", "50", "cost_usd", "0.42"), who)
	bronzeRow(&hooks, at.Add(time.Second), "",
		map[string]string{"hook_event_name": "UserPromptSubmit", "session_id": sessionA, "prompt_id": run + "-q1"}, who)
	// A repair that stopped after the copy: the named copy is already there beside the original.
	result := otelA("tool_result", "tool_name", "Read", "success", "true")
	bronzeRow(&logs, at.Add(10*time.Second), "", result, who)
	bronzeRow(&logs, at.Add(10*time.Second), "claude_code.tool_result", result, who)
	// A row with no name anywhere is not the repair's to touch.
	bronzeRow(&logs, at.Add(11*time.Second), "", map[string]string{"session.id": sessionA}, who)

	// Session B, too old for its trend row to be recomputed. Its rows are still repaired.
	bronzeRow(&logs, tooOld.Add(10*time.Hour), "", map[string]string{"event.name": "user_prompt",
		"session.id": sessionB, "prompt.id": run + "-q2", "user.pseudonym": "p-" + run}, who)

	if err := st.c.Insert(ctx, logsTable, logs.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := st.c.Insert(ctx, hooksTable, hooks.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, d := range []time.Time{touched, untouched, tooOld} {
		if err := st.c.Exec(ctx, fmt.Sprintf(
			"INSERT INTO cardo.trend_day (day, active_people, sessions, prompts, compactions, auto_compactions) "+
				"VALUES ('%s', 5, %d, 0, 0, 0)", day(d), marker)); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []time.Time{touched, tooOld} { // both are Mondays
		if err := st.c.Exec(ctx, fmt.Sprintf(
			"INSERT INTO cardo.trend_week_artifact (week, kind, artifact, file_kind, name_people, people, sessions, uses, active_people) "+
				"VALUES ('%s', 'skill', '%s', '', 5, 5, 5, %d, 5)", day(w), run, marker)); err != nil {
			t.Fatal(err)
		}
	}

	body, err := cardosql.ClickHouse.ReadFile("clickhouse/022_event_name_default.sql")
	if err != nil {
		t.Fatal(err)
	}
	for pass := 1; pass <= 2; pass++ {
		for i, stmt := range splitStatements(string(body)) {
			if err := st.c.Exec(ctx, stmt); err != nil {
				t.Fatalf("pass %d, statement %d: %v\n%s", pass, i+1, err, stmt)
			}
		}
	}

	for _, c := range []struct{ table, key, session, want string }{
		{logsTable, "session.id", sessionA,
			"(none)=1 claude_code.api_request=1 claude_code.tool_result=1 claude_code.user_prompt=1"},
		{hooksTable, "session_id", sessionA, "UserPromptSubmit=1"},
		{logsTable, "session.id", sessionB, "claude_code.user_prompt=1"},
	} {
		if got := eventNames(t, st, c.table, c.key, c.session); got != c.want {
			t.Errorf("%s, session %s, after two runs: %q, want %q", c.table, c.session, got, c.want)
		}
	}
	// The copy is the row, not a row like it.
	if got := trendQuery(t, st, fmt.Sprintf(
		"SELECT LogAttributes['cost_usd'], ResourceAttributes['cardo.salt_version'] FROM %s "+
			"WHERE LogAttributes['session.id'] = '%s' AND EventName = 'claude_code.api_request' FORMAT TabSeparated",
		logsTable, sessionA)); got != "0.42\tv1" {
		t.Errorf("the repaired request carries %q, want its cost and resource kept (0.42, v1)", got)
	}

	trendRows := func(table, key, d string, column string) string {
		return trendQuery(t, st, fmt.Sprintf(
			"SELECT count() FROM cardo.%s WHERE %s = '%s' AND %s = %d", table, key, d, column, marker))
	}
	for _, c := range []struct {
		what, table, key, d, column, want string
	}{
		{"the touched day", "trend_day", "day", day(touched), "sessions", "0"},
		{"the touched week", "trend_week_artifact", "week", day(touched), "uses", "0"},
		{"a day with no nameless row", "trend_day", "day", day(untouched), "sessions", "1"},
		{"a day outside the window", "trend_day", "day", day(tooOld), "sessions", "1"},
		{"a week outside the window", "trend_week_artifact", "week", day(tooOld), "uses", "1"},
	} {
		if got := trendRows(c.table, c.key, c.d, c.column); got != c.want {
			t.Errorf("%s (%s): %s trend row(s) left, want %s", c.what, c.d, got, c.want)
		}
	}

	// What the pages read: the prompt and its cost, back where they were missing.
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
	}
	turn := oneRow(t, st, "the repaired turn", fmt.Sprintf(
		"SELECT clock, api_requests, cost_usd FROM cardo.silver_turn WHERE session_id = '%s'", sessionA))
	expect(t, "the repaired turn", turn, map[string]string{"clock": "client", "api_requests": "1", "cost_usd": "0.42"})
	req := oneRow(t, st, "the repaired request", fmt.Sprintf(
		"SELECT count() AS n, sum(cost_usd) AS cost FROM cardo.silver_api_request WHERE session_id = '%s'", sessionA))
	expect(t, "the repaired request", req, map[string]string{"n": "1", "cost": "0.42"})
}
