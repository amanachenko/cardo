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

// trendDay is a day inside every retention window anyone runs, so the refresh is never refused.
const trendDay = "2026-10-20"

// seedTrendPeople writes one session per person on the given day, the way the collector would have
// stored them, and cleans up after itself.
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
	t.Cleanup(func() {
		for _, s := range sessions {
			_ = st.c.Exec(ctx, fmt.Sprintf(
				"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['session.id'] = '%s' SETTINGS mutations_sync = 1", s))
		}
		for _, tbl := range []string{"cardo.trend_day", "cardo.trend_day_cohort"} {
			_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION tuple('%s')", tbl, day))
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
			"AND table IN ('trend_day','trend_day_cohort','trend_week_artifact') ORDER BY table, name")
	if body == "" {
		t.Fatal("the trend tables have no columns; this test would pass vacuously")
	}
	forbidden := []string{"pseudonym", "email", "path", "body", "content", "salt"}
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
	for _, table := range []string{"cardo.trend_day", "cardo.trend_day_cohort"} {
		n := trendQuery(t, st,
			fmt.Sprintf("SELECT count() FROM %s WHERE day = '%s'", table, trendDay))
		if n != "0" {
			t.Errorf("%s holds %s row(s) for a day of two people. Two people is not a group; "+
				"a permanent record of their day is a person's record.", table, n)
		}
	}
}
