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
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/coach"
	cardosql "github.com/amanachenko/cardo/sql"
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
//     was exported at 10:05, inside b's part, and must not read as a switch back to a. Each account
//     carries its own team label, and a's first event has none: a's label is the first one seen.
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
	otel("switch", "10:01:00", "api_request", map[string]string{}, map[string]string{"user.pseudonym": a, "cardo.cohort": "red"})
	hook("switch", "10:02:00", "PermissionRequest")
	hook("switch", "10:03:00", "UserPromptSubmit")
	otel("switch", "10:03:00", "user_prompt", map[string]string{"user.pseudonym": b}, map[string]string{"cardo.cohort": "blue"})
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
		"SELECT session_id, pseudonym, %s, if(cohort = '', '(none)', cohort) FROM cardo.silver_session_account_live "+
			"WHERE startsWith(session_id, '%s') ORDER BY session_id, active_from FORMAT TabSeparated", from, run))
	want := "metric c start (none) | switch a start red | switch b 10:03:00 blue | switch a 10:06:00 red"
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

// switchSeed writes sessions in which the account can change: each OTel event names the account
// that sent it, or none, and a hook row names none, as a real one does not.
type switchSeed struct {
	t           *testing.T
	st          *Store
	run         string
	day         time.Time
	logs, hooks bytes.Buffer
}

func newSwitchSeed(t *testing.T, st *Store, prefix string, day time.Time) *switchSeed {
	s := &switchSeed{t: t, st: st, run: fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()), day: day}
	t.Cleanup(func() {
		ctx := context.Background()
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session.id'], '%s') SETTINGS mutations_sync = 1", logsTable, s.run))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DELETE WHERE startsWith(LogAttributes['session_id'], '%s') SETTINGS mutations_sync = 1", hooksTable, s.run))
		_ = RebuildSilver(ctx, st.c)
	})
	return s
}

func (s *switchSeed) name(n string) string { return s.run + "-" + n }

func (s *switchSeed) at(clock string) time.Time {
	d, err := time.ParseDuration(strings.Replace(strings.Replace(clock, ":", "h", 1), ":", "m", 1) + "s")
	if err != nil {
		s.t.Fatal(err)
	}
	return s.day.Add(d)
}

// otel adds an OTel event of the session sent by account, "" for none. Every event carries the
// team label and the salt version on the resource, as the bundle sets them.
func (s *switchSeed) otel(session, account, clock, event, prompt string, kv ...string) {
	attrs := map[string]string{"event.name": event, "session.id": s.name(session), "prompt.id": s.name(prompt), "app.version": "2.1.281"}
	if account != "" {
		attrs["user.pseudonym"] = s.name(account)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		attrs[kv[i]] = kv[i+1]
	}
	bronzeRow(&s.logs, s.at(clock), "claude_code."+event, attrs,
		map[string]string{"cardo.tier": "1", "cardo.salt_version": "v1", "cardo.cohort": s.name("team")})
}

func (s *switchSeed) hook(session, clock, event, prompt string, kv ...string) {
	attrs := map[string]string{"hook_event_name": event, "session_id": s.name(session)}
	if prompt != "" {
		attrs["prompt_id"] = s.name(prompt)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		attrs[kv[i]] = kv[i+1]
	}
	bronzeRow(&s.hooks, s.at(clock), event, attrs, map[string]string{"cardo.tier": "1", "cardo.salt_version": "v1"})
}

func (s *switchSeed) write() {
	s.t.Helper()
	ctx := context.Background()
	if err := s.st.c.Insert(ctx, logsTable, s.logs.Bytes()); err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.c.Insert(ctx, hooksTable, s.hooks.Bytes()); err != nil {
		s.t.Fatal(err)
	}
	if err := RebuildSilver(ctx, s.st.c); err != nil {
		s.t.Fatal(err)
	}
}

// seedSwitch writes one session that moves from account a to account b, and one session each for
// c, d and e, all five in one team, on one day eleven weeks back, clear of the other tests' days.
//
//	a  10:00  prompt q1, two requests, $0.10 and $0.20, after the session's start and an
//	          instructions file loaded before any OTel event
//	a  10:05  prompt q2, one request, $0.30
//	b  10:20  prompt q3, one request, $1.00, then a permission request, a Bash call and an Explore
//	          subagent, and the session's end
//	c, d, e   one prompt each, $0.05 each
func seedSwitch(t *testing.T, st *Store) *switchSeed {
	t.Helper()
	now := time.Now().UTC()
	monday := now.Truncate(24*time.Hour).AddDate(0, 0, -int((now.Weekday()+6)%7))
	s := newSwitchSeed(t, st, "two", monday.AddDate(0, 0, -7*11+2))

	s.hook("switch", "09:59:58", "SessionStart", "")
	s.hook("switch", "09:59:59", "InstructionsLoaded", "", "instructions_name", "acme-rules.md", "instructions_file", "project", "load_reason", "session_start")
	s.otel("switch", "a", "10:00:00", "user_prompt", "q1", "prompt_length", "40")
	s.hook("switch", "10:00:01", "UserPromptSubmit", "q1")
	s.otel("switch", "a", "10:00:05", "api_request", "q1", "query_source", "repl_main_thread", "model", "claude-sonnet-4-5", "input_tokens", "100", "cost_usd", "0.10")
	s.otel("switch", "a", "10:00:10", "api_request", "q1", "query_source", "repl_main_thread", "model", "claude-sonnet-4-5", "input_tokens", "200", "cost_usd", "0.20")
	s.otel("switch", "a", "10:05:00", "user_prompt", "q2", "prompt_length", "40")
	s.hook("switch", "10:05:01", "UserPromptSubmit", "q2")
	s.otel("switch", "a", "10:05:05", "api_request", "q2", "query_source", "repl_main_thread", "model", "claude-sonnet-4-5", "input_tokens", "300", "cost_usd", "0.30")
	s.otel("switch", "b", "10:20:00", "user_prompt", "q3", "prompt_length", "40")
	s.hook("switch", "10:20:01", "UserPromptSubmit", "q3")
	s.otel("switch", "b", "10:20:05", "api_request", "q3", "query_source", "repl_main_thread", "model", "claude-sonnet-4-5", "input_tokens", "400", "cost_usd", "1.00")
	s.hook("switch", "10:21:00", "PermissionRequest", "q3", "tool_name", "Bash", "permission_mode", "default")
	s.otel("switch", "b", "10:21:05", "tool_decision", "q3", "tool_name", "Bash", "tool_use_id", s.name("t1"), "decision", "accept", "source", "user_temporary")
	s.otel("switch", "b", "10:21:06", "tool_result", "q3", "tool_name", "Bash", "tool_use_id", s.name("t1"), "success", "true")
	s.hook("switch", "10:22:00", "SubagentStart", "q3", "agent_type", "Explore")
	s.hook("switch", "10:30:00", "SessionEnd", "", "session_end_reason", "prompt_input_exit")
	for _, p := range []string{"c", "d", "e"} {
		s.otel(p, p, "11:00:00", "user_prompt", p+"-q", "prompt_length", "40")
		s.otel(p, p, "11:00:05", "api_request", p+"-q", "query_source", "repl_main_thread", "model", "claude-sonnet-4-5", "input_tokens", "100", "cost_usd", "0.05")
	}
	s.write()
	return s
}

// A session that moved from one account to another (ADR-0059). Each account gets its own part,
// the fleet counts the session once, and every figure is the same however many times it is read:
// before, the session went to whichever account ClickHouse read first, and the coach changed on
// every reload.
func TestASessionWithTwoAccountsCountsOnceForEach(t *testing.T) {
	st := testStore(t)
	setViewSettings(t, st, ViewSettings{MinGroupSize: strPtr("5")})
	s := seedSwitch(t, st)
	ctx := context.Background()
	day, week := s.day.Format(time.DateOnly), s.day.AddDate(0, 0, -2).Format(time.DateOnly)
	endpoint, user, pass := os.Getenv("CARDO_CLICKHOUSE_URL"), os.Getenv("CARDO_CLICKHOUSE_USER"), os.Getenv("CARDO_CLICKHOUSE_PASSWORD")

	// The coach reads the _live views. Each account's page is its own part of the session, the
	// hook rows included: the instructions file loaded before any event is a's, the subagent
	// started after the switch is b's.
	wantCoach := map[string]struct {
		w    coach.Window
		arts []coach.Artifact
	}{
		"a": {coach.Window{Sessions: 1, Prompts: 2, CostUSD: 0.6, PeakContext: 250, ToolsObserved: true}, []coach.Artifact{{Name: "acme-rules.md", Loads: 1}}},
		"b": {coach.Window{Sessions: 1, Prompts: 1, CostUSD: 1, PeakContext: 400, ToolCalls: 1, ToolsObserved: true}, []coach.Artifact{{Name: "Explore", Loads: 1}}},
	}
	for read := 1; read <= 3; read++ {
		for account, want := range wantCoach {
			w, arts, err := coach.Query(ctx, endpoint, user, pass, s.name(account), s.day, s.day)
			if err != nil {
				t.Fatal(err)
			}
			if w != want.w || !reflect.DeepEqual(arts, want.arts) {
				t.Errorf("read %d, account %s: the coach shows %+v %v, want %+v %v", read, account, w, arts, want.w, want.arts)
			}
		}
	}

	for read := 1; read <= 3; read++ {
		// Each account's cost is its own requests.
		got := accountRows(t, st, s.run, fmt.Sprintf(
			"SELECT pseudonym, round(sum(cost_usd), 2) FROM cardo.silver_api_request WHERE session_id = '%s' "+
				"GROUP BY pseudonym ORDER BY pseudonym FORMAT TabSeparated", s.name("switch")))
		if want := "a 0.6 | b 1"; got != want {
			t.Errorf("read %d: cost by account %q, want %q", read, got, want)
		}

		// One row per account, both on the session's day. The session ended in b's part.
		got = accountRows(t, st, s.run, fmt.Sprintf(
			"SELECT pseudonym, day = '%s', ended, if(end_reason = '', '-', end_reason), cohort FROM cardo.silver_session "+
				"WHERE session_id = '%s' ORDER BY pseudonym FORMAT TabSeparated", day, s.name("switch")))
		if want := "a 1 0 - team | b 1 1 prompt_input_exit team"; got != want {
			t.Errorf("read %d: the session's rows %q, want %q", read, got, want)
		}

		// A hook row after the switch is b's: the permission wait, and the subagent.
		got = accountRows(t, st, s.run, fmt.Sprintf(
			"SELECT what, pseudonym FROM (SELECT 'wait' AS what, pseudonym FROM cardo.silver_policy_decision WHERE session_id = '%[1]s' "+
				"UNION ALL SELECT name, pseudonym FROM cardo.silver_artifact_load WHERE session_id = '%[1]s') ORDER BY what FORMAT TabSeparated", s.name("switch")))
		if want := "Explore b | acme-rules.md a | wait b"; got != want {
			t.Errorf("read %d: hook rows credited %q, want %q", read, got, want)
		}

		// The team has five people, a and b among them, so it is named. Counted with one account,
		// as before, it had four and was "other". The session counts once for the team, and once for
		// the fleet, beside c's, d's and e's.
		expect(t, "the team's day", oneRow(t, st, "the team's day", fmt.Sprintf(
			"SELECT active_people, sessions, prompts, permission_prompts FROM cardo.gold_friction_daily WHERE day = '%s' AND cohort = '%s'", day, s.name("team"))),
			map[string]string{"active_people": "5", "sessions": "4", "prompts": "6", "permission_prompts": "1"})
		expect(t, "the team's week", oneRow(t, st, "the team's week", fmt.Sprintf(
			"SELECT sessions, requests, cost_usd FROM cardo.gold_spend_weekly WHERE week = '%s' AND cohort = '%s'", week, s.name("team"))),
			map[string]string{"sessions": "4", "requests": "7", "cost_usd": "1.75"})
		expect(t, "the trend's day", oneRow(t, st, "the trend's day", fmt.Sprintf(
			"SELECT active_people, sessions, prompts FROM cardo.trend_day_source WHERE day = '%s'", day)),
			map[string]string{"active_people": "5", "sessions": "4", "prompts": "6"})
		// Fleet adoption adds up each person's sessions: the session is a's, who started it, and b
		// still counts as active.
		expect(t, "fleet adoption", oneRow(t, st, "fleet adoption", fmt.Sprintf(
			"SELECT active_actors, sessions FROM cardo.gold_fleet_adoption WHERE day = '%s' AND source = 'sessions'", day)),
			map[string]string{"active_actors": "5", "sessions": "4"})
	}
}

// The trend rows written while a switched session counted for one account are cleared for
// `cardo trend` to recompute (023, section 5), and the rows of other days are left alone.
func TestTheTrendRowsOfASwitchedSessionAreRecomputed(t *testing.T) {
	st := testStore(t)
	s := seedSwitch(t, st)
	ctx := context.Background()
	const marker = 987654321
	day, other := s.day, s.day.AddDate(0, 0, 1)
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.trend_day DELETE WHERE active_people = %d SETTINGS mutations_sync = 2", marker))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.trend_week_artifact DELETE WHERE active_people = %d SETTINGS mutations_sync = 2", marker))
	})
	for _, q := range []string{
		fmt.Sprintf("INSERT INTO cardo.trend_day VALUES ('%s', %d, 1, 1, 0, 0), ('%s', %d, 1, 1, 0, 0)",
			day.Format(time.DateOnly), marker, other.Format(time.DateOnly), marker),
		fmt.Sprintf("INSERT INTO cardo.trend_week_artifact VALUES ('%s', 'skill', 'x', '', 5, 5, 1, 1, %d), ('%s', 'skill', 'x', '', 5, 5, 1, 1, %d)",
			day.AddDate(0, 0, -2).Format(time.DateOnly), marker, day.AddDate(0, 0, 5).Format(time.DateOnly), marker),
	} {
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	body, err := cardosql.ClickHouse.ReadFile("clickhouse/023_one_account_per_row.sql")
	if err != nil {
		t.Fatal(err)
	}
	var section5 []string
	for _, stmt := range splitStatements(string(body)) {
		if strings.Contains(stmt, "repair_023_days") {
			section5 = append(section5, stmt)
		}
	}
	if len(section5) != 6 {
		t.Fatalf("found %d statements of section 5, want 6", len(section5))
	}
	for run := 1; run <= 2; run++ {
		for _, stmt := range section5 {
			if err := st.c.Exec(ctx, stmt); err != nil {
				t.Fatalf("run %d: %v\n%s", run, err, stmt)
			}
		}
	}

	got := trendQuery(t, st, fmt.Sprintf("SELECT groupArray(toString(day)) FROM (SELECT day FROM cardo.trend_day WHERE active_people = %d ORDER BY day)", marker))
	if want := fmt.Sprintf("['%s']", other.Format(time.DateOnly)); got != want {
		t.Errorf("trend days left %s, want only the day without the switched session, %s", got, want)
	}
	got = trendQuery(t, st, fmt.Sprintf("SELECT groupArray(toString(week)) FROM (SELECT week FROM cardo.trend_week_artifact WHERE active_people = %d ORDER BY week)", marker))
	if want := fmt.Sprintf("['%s']", day.AddDate(0, 0, 5).Format(time.DateOnly)); got != want {
		t.Errorf("trend weeks left %s, want only the next week, %s", got, want)
	}
}

// deletionSteps is the deletion procedure in docs/design/privacy.md, steps 1 and 2, as an
// operator runs it for one person: sessions is the list step 1 printed, written as SQL.
func deletionSteps(pseudo, sessions string) []string {
	return []string{
		fmt.Sprintf("CREATE TABLE cardo.deletion_credited ENGINE = MergeTree ORDER BY tuple() AS "+
			"SELECT 'hook' AS kind, r.LogAttributes['session_id'] AS session_id, r.Timestamp AS at, cityHash64(r.LogAttributes) AS attributes "+
			"FROM cardo.bronze_hook_events AS r ASOF JOIN cardo.silver_session_account_live AS a "+
			"ON a.session_id = r.LogAttributes['session_id'] AND r.Timestamp >= a.active_from "+
			"WHERE r.LogAttributes['session_id'] IN (%[2]s) AND a.pseudonym = '%[1]s' "+
			"UNION ALL "+
			"SELECT 'otel', r.LogAttributes['session.id'], r.Timestamp, cityHash64(r.LogAttributes) "+
			"FROM cardo.bronze_otel_logs AS r ASOF JOIN cardo.silver_session_account_live AS a "+
			"ON a.session_id = r.LogAttributes['session.id'] AND r.Timestamp >= a.active_from "+
			"WHERE r.LogAttributes['session.id'] IN (%[2]s) "+
			"AND r.LogAttributes['user.pseudonym'] = '' AND r.ResourceAttributes['user.pseudonym'] = '' "+
			"AND a.pseudonym = '%[1]s'", pseudo, sessions),
		fmt.Sprintf("ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['user.pseudonym'] = '%[1]s' "+
			"OR ResourceAttributes['user.pseudonym'] = '%[1]s' "+
			"OR (LogAttributes['session.id'], Timestamp, cityHash64(LogAttributes)) IN "+
			"(SELECT session_id, at, attributes FROM cardo.deletion_credited WHERE kind = 'otel') SETTINGS mutations_sync = 1", pseudo),
		fmt.Sprintf("ALTER TABLE cardo.bronze_otel_metrics_sum DELETE WHERE Attributes['user.pseudonym'] = '%[1]s' "+
			"OR ResourceAttributes['user.pseudonym'] = '%[1]s' SETTINGS mutations_sync = 1", pseudo),
		"ALTER TABLE cardo.bronze_hook_events DELETE WHERE (LogAttributes['session_id'], Timestamp, cityHash64(LogAttributes)) IN " +
			"(SELECT session_id, at, attributes FROM cardo.deletion_credited WHERE kind = 'hook') SETTINGS mutations_sync = 1",
		fmt.Sprintf("ALTER TABLE cardo.bronze_actor_day DELETE WHERE pseudonym = '%s' SETTINGS mutations_sync = 1", pseudo),
		fmt.Sprintf("ALTER TABLE cardo.settings_teams DELETE WHERE pseudonym = '%s' SETTINGS mutations_sync = 1", pseudo),
		"DROP TABLE cardo.deletion_credited",
	}
}

// Deleting the account a session moved to deletes that account's part and nothing of the other's:
// its events, the hook rows sent while it was active, and its row in silver. Deleting every hook row
// of the person's sessions, as the procedure did, took the first account's start, instructions
// file and prompts' hooks with it.
func TestDeletingOneAccountLeavesTheOthersPart(t *testing.T) {
	st := testStore(t)
	s := seedSwitch(t, st)
	ctx := context.Background()
	t.Cleanup(func() { _ = st.c.Exec(ctx, "DROP TABLE IF EXISTS cardo.deletion_credited") })

	sessions := strings.TrimSpace(trendQuery(t, st, fmt.Sprintf(
		"SELECT DISTINCT session_id FROM cardo.silver_session_live WHERE pseudonym = '%s' FORMAT TSV", s.name("b"))))
	if sessions != s.name("switch") {
		t.Fatalf("step 1 lists %q, want the switched session", sessions)
	}
	for _, q := range deletionSteps(s.name("b"), "'"+sessions+"'") {
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatalf("a deletion step failed: %v\n%s", err, q)
		}
	}
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
	}

	left := func(q string) string {
		return accountRows(t, st, s.run, q+" FORMAT TabSeparated")
	}
	if got, want := left(fmt.Sprintf("SELECT EventName, if(prompt = '', '-', prompt) FROM (SELECT EventName, LogAttributes['prompt_id'] AS prompt, Timestamp FROM %s "+
		"WHERE LogAttributes['session_id'] = '%s') ORDER BY Timestamp", hooksTable, s.name("switch"))),
		"SessionStart - | InstructionsLoaded - | UserPromptSubmit q1 | UserPromptSubmit q2"; got != want {
		t.Errorf("hook rows left %q, want a's only %q", got, want)
	}
	if got, want := left(fmt.Sprintf("SELECT LogAttributes['user.pseudonym'], count() FROM %s WHERE LogAttributes['session.id'] = '%s' "+
		"GROUP BY 1 ORDER BY 1", logsTable, s.name("switch"))), "a 5"; got != want {
		t.Errorf("OTel events left %q, want a's five %q", got, want)
	}
	if got := left(fmt.Sprintf("SELECT ifNull(pseudonym, '(none)') FROM cardo.silver_session WHERE session_id = '%s'", s.name("switch"))); got != "a" {
		t.Errorf("the session's rows after the deletion %q, want a's only", got)
	}
}
