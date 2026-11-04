package clickhouse

// A session can carry more than one account (ADR-0059, sql/clickhouse/023).
//
// When someone switches Claude accounts in the middle of a session, the session id stays and the
// code name on its OTel events changes. Each row belongs to the account on it, and a row with no
// code name, such as every hook row, to the account active when it was sent. These tests run
// against a real ClickHouse and are skipped unless CARDO_CLICKHOUSE_URL is set:
//
//	CARDO_CLICKHOUSE_URL=... CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
//	  go test ./internal/store/clickhouse/ -run Account -count=1

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// accountRows reads a query's TSV rows, one string each, with the run's prefix removed so the
// expectations read as the scenario does.
func accountRows(t *testing.T, st *Store, run, sql string) string {
	t.Helper()
	out := strings.ReplaceAll(trendQuery(t, st, sql), run+"-", "")
	return strings.Join(strings.Split(strings.ReplaceAll(out, "\t", " "), "\n"), " | ")
}

// The helper view lists when each account became active, and an unnamed row takes the account
// active at its moment. The scenario, on one day three weeks back:
//
//   - session "switch": account a from 10:00, b from 10:03, a again from 10:06. Its hook rows fall
//     before any OTel event, inside each part, and after the switch back. Its session-count metric
//     was exported at 10:05, inside b's part, and must not read as a switch back to a.
//   - session "metric": its only code name is on the session-count metric, on the resource.
//   - session "none": no code name anywhere, as with an API key. Its rows stay unattributed.
func TestTheAccountActiveWhenARowWasSent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	run := fmt.Sprintf("acct-%d", time.Now().UnixNano())
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -19)
	at := func(clock string) time.Time {
		d, err := time.ParseDuration(strings.Replace(strings.Replace(clock, ":", "h", 1), ":", "m", 1) + "s")
		if err != nil {
			t.Fatal(err)
		}
		return day.Add(d)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			fmt.Sprintf("ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session.id'], '%s') SETTINGS mutations_sync = 1", logsTable, run),
			fmt.Sprintf("ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session_id'], '%s') SETTINGS mutations_sync = 1", hooksTable, run),
			fmt.Sprintf("ALTER TABLE cardo.bronze_otel_metrics_sum DELETE WHERE startsWith(Attributes['session.id'], '%s') SETTINGS mutations_sync = 1", run),
		} {
			_ = st.c.Exec(ctx, q)
		}
	})

	var logs, hooks bytes.Buffer
	otel := func(session, clock, event string, attrs, resource map[string]string) {
		attrs["session.id"], attrs["event.name"] = run+"-"+session, event
		bronzeRow(&logs, at(clock), "claude_code."+event, attrs, resource)
	}
	hook := func(session, clock, event string) {
		bronzeRow(&hooks, at(clock), event,
			map[string]string{"session_id": run + "-" + session, "hook_event_name": event}, map[string]string{})
	}
	a, b, c := run+"-a", run+"-b", run+"-c"

	hook("switch", "09:59:00", "SessionStart")
	otel("switch", "10:00:00", "user_prompt", map[string]string{"user.pseudonym": a}, map[string]string{})
	// The collector may put the code name on the resource instead of the record.
	otel("switch", "10:01:00", "api_request", map[string]string{}, map[string]string{"user.pseudonym": a})
	hook("switch", "10:02:00", "PermissionRequest")
	hook("switch", "10:03:00", "UserPromptSubmit")
	otel("switch", "10:03:00", "user_prompt", map[string]string{"user.pseudonym": b}, map[string]string{})
	otel("switch", "10:04:00", "api_request", map[string]string{"user.pseudonym": b}, map[string]string{})
	hook("switch", "10:05:00", "PermissionRequest")
	otel("switch", "10:06:00", "user_prompt", map[string]string{"user.pseudonym": a}, map[string]string{})
	// An OTel event with no code name is credited like a hook row.
	otel("switch", "10:06:30", "tool_result", map[string]string{}, map[string]string{})
	hook("switch", "10:07:00", "SessionEnd")

	hook("metric", "12:00:30", "UserPromptSubmit")

	otel("none", "13:00:00", "user_prompt", map[string]string{}, map[string]string{})
	hook("none", "13:00:01", "UserPromptSubmit")

	if err := st.c.Insert(ctx, logsTable, logs.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := st.c.Insert(ctx, hooksTable, hooks.Bytes()); err != nil {
		t.Fatal(err)
	}
	// A metric point's interval starts at StartTimeUnix and ends, when it is exported, at TimeUnix.
	for _, m := range []struct{ session, attrs, resource, start, end string }{
		{"switch", mapLiteral(map[string]string{"session.id": run + "-switch", "user.pseudonym": a}), "{}", "09:59:50", "10:05:00"},
		{"metric", mapLiteral(map[string]string{"session.id": run + "-metric"}), mapLiteral(map[string]string{"user.pseudonym": c}), "12:00:00", "12:01:00"},
	} {
		q := fmt.Sprintf("INSERT INTO cardo.bronze_otel_metrics_sum (MetricName, Attributes, ResourceAttributes, StartTimeUnix, TimeUnix, Value) "+
			"VALUES ('claude_code.session.count', %s, %s, toDateTime('%s', 'UTC'), toDateTime('%s', 'UTC'), 1)",
			m.attrs, m.resource, at(m.start).Format(time.DateTime), at(m.end).Format(time.DateTime))
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}

	from := "if(active_from = toDateTime64(0, 9), 'start', formatDateTime(active_from, '%H:%i:%S', 'UTC'))"
	got := accountRows(t, st, run, fmt.Sprintf(
		"SELECT session_id, pseudonym, %s FROM cardo.silver_session_account_live "+
			"WHERE startsWith(session_id, '%s') ORDER BY session_id, active_from FORMAT TabSeparated", from, run))
	want := "metric c start | switch a start | switch b 10:03:00 | switch a 10:06:00"
	if got != want {
		t.Errorf("when each account became active:\n got %s\nwant %s", got, want)
	}

	// The lookup every view makes for an unnamed row, here for the hook rows and the OTel event
	// that came without a code name.
	credited := func(table, key, where string) string {
		return accountRows(t, st, run, fmt.Sprintf(
			"SELECT r.LogAttributes['%[2]s'], formatDateTime(r.Timestamp, '%%H:%%i:%%S', 'UTC'), if(acct.pseudonym = '', '(none)', acct.pseudonym) "+
				"FROM %[1]s AS r ASOF LEFT JOIN cardo.silver_session_account_live AS acct "+
				"ON acct.session_id = r.LogAttributes['%[2]s'] AND r.Timestamp >= acct.active_from "+
				"WHERE startsWith(r.LogAttributes['%[2]s'], '%[3]s') AND %[4]s ORDER BY r.Timestamp FORMAT TabSeparated",
			table, key, run, where))
	}
	if got, want := credited(hooksTable, "session_id", "1"),
		"switch 09:59:00 a | switch 10:02:00 a | switch 10:03:00 b | switch 10:05:00 b | switch 10:07:00 a | "+
			"metric 12:00:30 c | none 13:00:01 (none)"; got != want {
		t.Errorf("hook rows credited:\n got %s\nwant %s", got, want)
	}
	if got, want := credited(logsTable, "session.id",
		"r.LogAttributes['user.pseudonym'] = '' AND r.ResourceAttributes['user.pseudonym'] = ''"),
		"switch 10:06:30 a | none 13:00:00 (none)"; got != want {
		t.Errorf("OTel events without a code name credited:\n got %s\nwant %s", got, want)
	}
}
