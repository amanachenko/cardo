package clickhouse

// The acceptance checks for the trend rows that outlive their rows (ADR-0055).
//
// These tests run against a real ClickHouse and are skipped unless CARDO_CLICKHOUSE_URL is set:
//
//	CARDO_CLICKHOUSE_URL=... CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
//	  go test ./internal/store/clickhouse/ -run TestTrend -count=1
//
// -count=1 matters here: Go's test cache does not know about the network, and a cached PASS
// against a changed server looks exactly like a real one.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// trendDay is yesterday: inside every retention window anyone runs, so the refresh is never
// refused. A fixed date would start being refused as the window moves past it.
var trendDay = time.Now().AddDate(0, 0, -1).Format("2006-01-02")

// seedTrendPeople writes one session per person on the given day, the way the collector would have
// stored them, and cleans up after itself.
//
// The views read stored silver, so the rows are rebuilt before the helper returns and again after
// the clean-up deletes them. The clean-up then recomputes the day rather than dropping its
// partition: on a developer's own stack those trend rows are real ones.
func seedTrendPeople(t *testing.T, st *Store, day string, people int) []string {
	t.Helper()
	ctx := context.Background()
	stamp := fmt.Sprintf("trend%d", time.Now().UnixNano())

	var sessions []string
	for p := 0; p < people; p++ {
		pseudo := fmt.Sprintf("%064x", time.Now().UnixNano()+int64(p))
		session := fmt.Sprintf("%s-s%d", stamp, p)
		sessions = append(sessions, session)
		otel := fmt.Sprintf(
			"INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, "+
				"ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) "+
				"VALUES (toDateTime64('%s 10:00:00',9),'claude-code','','x',"+
				"{'cardo.salt_version':'v1','user.pseudonym':'%s'},'','','',{},"+
				"{'session.id':'%s','user.pseudonym':'%s','cardo.cohort':'acme-payments'},'claude_code.user_prompt')",
			day, pseudo, session, pseudo)
		if err := st.c.Exec(ctx, otel); err != nil {
			t.Fatalf("cannot seed the session: %v", err)
		}
	}
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, s := range sessions {
			_ = st.c.Exec(ctx, fmt.Sprintf(
				"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['session.id'] = '%s' SETTINGS mutations_sync = 1", s))
		}
		if err := RebuildSilver(ctx, st.c); err != nil {
			t.Errorf("cannot rebuild silver after the clean-up: %v", err)
		}
		if err := st.Trend(ctx, day); err != nil {
			t.Errorf("cannot recompute %s in the clean-up: %v", day, err)
		}
	})
	return sessions
}

// trendQuery runs one query and returns its TSV body.
func trendQuery(t *testing.T, st *Store, sql string) string {
	t.Helper()
	body, err := st.c.Query(context.Background(), sql)
	if err != nil {
		t.Fatalf("query failed: %v\n%s", err, sql)
	}
	return strings.TrimSpace(string(body))
}

// The table is the whole privacy argument, so its columns are checked rather than trusted. A row
// here is kept for ever; if a column could name a person, keeping it would break the tier rule
// (ADR-0016) that makes keeping it legal in the first place.
func TestTrendTablesCarryNoPseudonymAndNoContent(t *testing.T) {
	st := testStore(t)
	body := trendQuery(t, st,
		"SELECT table, name FROM system.columns WHERE database = 'cardo' "+
			"AND table IN ('trend_day','trend_week_artifact') ORDER BY table, name")
	if body == "" {
		t.Fatal("the trend tables have no columns; this test would pass vacuously")
	}
	forbidden := []string{"pseudonym", "email", "path", "body", "content", "salt", "cohort", "team"}
	for _, line := range strings.Split(body, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected row from system.columns: %q", line)
		}
		table, column := parts[0], parts[1]
		for _, f := range forbidden {
			if strings.Contains(strings.ToLower(column), f) {
				t.Errorf("INV-5 violated: %s.%s carries %q in a table retained for ever.\n"+
					"  A trend row is kept past the retention window precisely because it names nobody.\n"+
					"  A column that could name a person makes the whole table tier 1, and tier 1 expires.",
					table, column, f)
			}
		}
	}
}

// The check that would prove the item closed: a trend older than the window is still there.
func TestTrendOutlivesTheRowsBehindThem(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	sessions := seedTrendPeople(t, st, trendDay, 5)

	if err := st.Trend(ctx, trendDay); err != nil {
		t.Fatalf("recomputing %s: %v", trendDay, err)
	}
	const want = "5\t5"
	got := trendQuery(t, st,
		fmt.Sprintf("SELECT active_people, sessions FROM cardo.gold_trend_day WHERE day = '%s'", trendDay))
	if got != want {
		t.Fatalf("the trend row is %q, want %q", got, want)
	}

	// Now the rows behind it expire. Nothing rebuilds the aggregate from what is left: the
	// aggregate is what is left.
	for _, s := range sessions {
		if err := st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['session.id'] = '%s' SETTINGS mutations_sync = 1", s)); err != nil {
			t.Fatalf("expiring the rows behind %s: %v", trendDay, err)
		}
	}
	// The stored silver tables hold what the last rebuild saw, so they are rebuilt away too
	// before the count below reads them.
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
	}
	in := "'" + strings.Join(sessions, "','") + "'"
	if n := trendQuery(t, st,
		fmt.Sprintf("SELECT count() FROM cardo.silver_session WHERE session_id IN (%s)", in)); n != "0" {
		t.Fatalf("the rows behind %s are still there (%s of them); this check would prove nothing", trendDay, n)
	}
	after := trendQuery(t, st,
		fmt.Sprintf("SELECT active_people, sessions FROM cardo.gold_trend_day WHERE day = '%s'", trendDay))
	if after != want {
		t.Fatalf("the trend row went away with the rows behind it: got %q, want %q", after, want)
	}
}

// A refresh that fails partway must leave the previous day's numbers in place, not an empty day:
// an empty day on a dashboard looks exactly like a quiet one.
func TestTrendRefreshLeavesThePreviousNumbersInPlace(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	seedTrendPeople(t, st, trendDay, 5)

	if err := st.Trend(ctx, trendDay); err != nil {
		t.Fatalf("recomputing %s: %v", trendDay, err)
	}
	const want = "5\t5"

	// Break the computation the refresh reads, the way a real failure would break it halfway.
	if err := st.c.Exec(ctx, "RENAME TABLE cardo.trend_day_source TO cardo.trend_day_source_broken"); err != nil {
		t.Fatalf("breaking the source: %v", err)
	}
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, "RENAME TABLE cardo.trend_day_source_broken TO cardo.trend_day_source")
	})

	if err := st.Trend(ctx, trendDay); err == nil {
		t.Fatal("the refresh reported success with nothing to compute; a failed refresh must say so")
	}
	got := trendQuery(t, st,
		fmt.Sprintf("SELECT active_people, sessions FROM cardo.gold_trend_day WHERE day = '%s'", trendDay))
	if got != want {
		t.Fatalf("a refresh that failed partway changed the day: got %q, want %q", got, want)
	}
}

// A row describing fewer than five people is a person's record, and a person's record expires with
// the window. Nothing below the floor is written at all (ADR-0029).
func TestTrendKeepsNothingBelowTheFloor(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	seedTrendPeople(t, st, trendDay, 2)

	if err := st.Trend(ctx, trendDay); err != nil {
		t.Fatalf("recomputing %s: %v", trendDay, err)
	}
	n := trendQuery(t, st,
		fmt.Sprintf("SELECT count() FROM cardo.trend_day WHERE day = '%s'", trendDay))
	if n != "0" {
		t.Errorf("trend_day holds %s row(s) for a day of two people. Two people is not a group; "+
			"a permanent record of their day is a person's record.", n)
	}
}

// A service run is not a person (ADR-0036, ADR-0054), and the live views leave it out of every
// count. A file loaded by four people and one CI job is four people.
func TestServiceRunsAreNotPeopleInTrendRows(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	seedTrendPeople(t, st, trendDay, 5)

	// One CI job on the same day: labelled by the run itself, as the collector writes it.
	stamp := fmt.Sprintf("trendsvc%d", time.Now().UnixNano())
	if err := st.c.Exec(ctx, fmt.Sprintf(
		"INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, "+
			"ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) "+
			"VALUES (toDateTime64('%s 10:00:00',9),'claude-code','','x',"+
			"{'cardo.salt_version':'v1','user.pseudonym':'%064x','cardo.actor':'service'},'','','',{},"+
			"{'session.id':'%s-s0','user.pseudonym':'%064x','cardo.cohort':'acme-payments'},'claude_code.user_prompt')",
		trendDay, time.Now().UnixNano(), stamp, time.Now().UnixNano())); err != nil {
		t.Fatalf("cannot seed the service run: %v", err)
	}
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['session.id'] = '%s-s0' SETTINGS mutations_sync = 1", stamp))
	})

	if err := st.Trend(ctx, trendDay); err != nil {
		t.Fatalf("recomputing %s: %v", trendDay, err)
	}
	got := trendQuery(t, st,
		fmt.Sprintf("SELECT active_people, sessions FROM cardo.gold_trend_day WHERE day = '%s'", trendDay))
	if want := "5\t5"; got != want {
		t.Errorf("a service run counted as a person: %q, want %q", got, want)
	}
}

// A stack that was stopped for a week has to catch up in one run. The backfill fills every day
// inside the retention window with no row yet, so the days it missed do not stay empty for ever
// once the rows behind them expire (ADR-0055's hole rule).
func TestTrendBackfillFillsTheDaysTheStackMissed(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// A short window keeps the backfill to a handful of days. It is put back on the way out.
	prev := trendQuery(t, st, "SELECT argMax(value, version) FROM cardo.settings WHERE key = 'retention_days' FORMAT TabSeparated")
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf(
			"INSERT INTO cardo.settings (key, value, version) SELECT 'retention_days', '%s', max(version) + 1 FROM cardo.settings", prev))
	})
	if err := st.c.Exec(ctx,
		"INSERT INTO cardo.settings (key, value, version) SELECT 'retention_days', '12', max(version) + 1 FROM cardo.settings"); err != nil {
		t.Fatal(err)
	}

	// Ten days of five people each, none of them computed: the stack was stopped.
	for i := 1; i <= 10; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		seedTrendPeople(t, st, day, 5)
	}

	if _, err := st.TrendBackfill(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	first := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	n := trendQuery(t, st,
		fmt.Sprintf("SELECT count(DISTINCT day) FROM cardo.trend_day WHERE day BETWEEN '%s' AND '%s'", first, trendDay))
	if n != "10" {
		t.Errorf("the backfill filled %s of the ten missed days, want 10", n)
	}
}

// The deletion path in docs/design/privacy.md is a procedure somebody will run by hand, so its
// steps are run here and checked against every table: the pseudonym reaches a row through the
// record, through the resource, through a metric point and through a session id alone, and the old
// one-field delete left three of those four in the store.
func TestTheDeletionStepsLeaveNothingOfThePerson(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	nano := time.Now().UnixNano()
	pseudo := fmt.Sprintf("%064x", nano)
	other := fmt.Sprintf("%064x", nano+1)
	session := fmt.Sprintf("del%d-s0", nano)
	otherSession := fmt.Sprintf("del%d-s1", nano)

	// Their rows in every shape the collector writes.
	rows := []string{
		// the pseudonym on the record only
		fmt.Sprintf("INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) VALUES (toDateTime64('%s 10:00:00',9),'claude-code','','x',{'cardo.salt_version':'v1'},'','','',{},{'session.id':'%s','user.pseudonym':'%s'},'claude_code.user_prompt')", trendDay, session, pseudo),
		// the pseudonym on the resource only
		fmt.Sprintf("INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) VALUES (toDateTime64('%s 10:01:00',9),'claude-code','','x',{'user.pseudonym':'%s'},'','','',{},{'session.id':'%s'},'claude_code.user_prompt')", trendDay, pseudo, session),
		// a metric point, the pseudonym on the data point
		fmt.Sprintf("INSERT INTO cardo.bronze_otel_metrics_sum (MetricName, Attributes, TimeUnix, Value) VALUES ('claude_code.session.count',{'session.id':'%s','user.pseudonym':'%s'},toDateTime('%s 10:02:00'),1)", session, pseudo, trendDay),
		// a hook row: no identity at all, only the session id
		fmt.Sprintf("INSERT INTO cardo.bronze_hook_events (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) VALUES (toDateTime64('%s 10:03:00',9),'claude-code','','x',{},'','','',{},{'session_id':'%s','instructions_name':'acme-rules-v1.md'},'InstructionsLoaded')", trendDay, session),
		// the day table's own pseudonym column
		fmt.Sprintf("INSERT INTO cardo.bronze_actor_day (source, day, pseudonym, salt_version, tier, actor_type) VALUES ('test_delete','%s','%s','v1',1,'user_actor')", trendDay, pseudo),
		// someone else, who must survive
		fmt.Sprintf("INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) VALUES (toDateTime64('%s 10:04:00',9),'claude-code','','x',{'cardo.salt_version':'v1'},'','','',{},{'session.id':'%s','user.pseudonym':'%s'},'claude_code.user_prompt')", trendDay, otherSession, other),
	}
	for _, q := range rows {
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatalf("cannot seed the person: %v", err)
		}
	}
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['session.id'] = '%s' SETTINGS mutations_sync = 1", otherSession))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['session.id'] = '%s' OR ResourceAttributes['user.pseudonym'] = '%s' SETTINGS mutations_sync = 1", session, pseudo))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.bronze_otel_metrics_sum DELETE WHERE Attributes['session.id'] = '%s' SETTINGS mutations_sync = 1", session))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.bronze_hook_events DELETE WHERE LogAttributes['session_id'] = '%s' SETTINGS mutations_sync = 1", session))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.bronze_actor_day DELETE WHERE pseudonym = '%s' OR pseudonym = '%s' SETTINGS mutations_sync = 1", pseudo, other))
		_ = RebuildSilver(ctx, st.c)
	})

	// The documented steps.
	sessions := strings.TrimSpace(trendQuery(t, st,
		fmt.Sprintf("SELECT session_id FROM cardo.silver_session_live WHERE pseudonym = '%s' FORMAT TSV", pseudo)))
	if sessions != session {
		t.Fatalf("step 1 lists %q, want the person's session %q", sessions, session)
	}
	steps := []string{
		fmt.Sprintf("ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['user.pseudonym'] = '%s' OR ResourceAttributes['user.pseudonym'] = '%s' OR LogAttributes['session.id'] IN ('%s') SETTINGS mutations_sync = 1", pseudo, pseudo, sessions),
		fmt.Sprintf("ALTER TABLE cardo.bronze_otel_metrics_sum DELETE WHERE Attributes['user.pseudonym'] = '%s' OR ResourceAttributes['user.pseudonym'] = '%s' SETTINGS mutations_sync = 1", pseudo, pseudo),
		fmt.Sprintf("ALTER TABLE cardo.bronze_hook_events DELETE WHERE LogAttributes['session_id'] IN ('%s') SETTINGS mutations_sync = 1", sessions),
		fmt.Sprintf("ALTER TABLE cardo.bronze_actor_day DELETE WHERE pseudonym = '%s' SETTINGS mutations_sync = 1", pseudo),
	}
	for _, q := range steps {
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatalf("a deletion step failed: %v\n%s", err, q)
		}
	}
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		"SELECT count() FROM cardo.bronze_otel_logs WHERE LogAttributes['user.pseudonym'] = '" + pseudo + "' OR ResourceAttributes['user.pseudonym'] = '" + pseudo + "' OR LogAttributes['session.id'] = '" + session + "'",
		"SELECT count() FROM cardo.bronze_otel_metrics_sum WHERE Attributes['user.pseudonym'] = '" + pseudo + "' OR Attributes['session.id'] = '" + session + "'",
		"SELECT count() FROM cardo.bronze_hook_events WHERE LogAttributes['session_id'] = '" + session + "'",
		"SELECT count() FROM cardo.bronze_actor_day WHERE pseudonym = '" + pseudo + "'",
		"SELECT count() FROM cardo.silver_session WHERE session_id = '" + session + "'",
	} {
		if n := trendQuery(t, st, q+" FORMAT TabSeparated"); n != "0" {
			t.Errorf("the deletion left %s row(s) behind: %s", n, q)
		}
	}
	if n := trendQuery(t, st,
		"SELECT count() FROM cardo.bronze_otel_logs WHERE LogAttributes['session.id'] = '"+otherSession+"' FORMAT TabSeparated"); n != "1" {
		t.Errorf("the deletion took someone else's rows too: %s left, want 1", n)
	}
}
