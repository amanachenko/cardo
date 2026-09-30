package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
)

// These tests run against a real ClickHouse and are skipped unless CARDO_CLICKHOUSE_URL is set,
// so `go test ./...` stays green on a machine with no Docker:
//
//	docker compose -f deploy/compose/docker-compose.yml up -d clickhouse
//	CARDO_CLICKHOUSE_URL=... CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
//	  go test ./internal/store/clickhouse/ -v
//
// They write under a `test_console` source so they can share a database with real data without
// touching its partitions, and they drop what they wrote on the way out.

const (
	testSalt   = "0123456789abcdef0123456789abcdef"
	testSource = source.Name("test_console")
	testEmail  = "veryspecific.person@example-corp.com"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	endpoint := os.Getenv("CARDO_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CARDO_CLICKHOUSE_URL not set; skipping the ClickHouse integration tests")
	}
	st, err := New(Config{
		Endpoint: endpoint,
		User:     os.Getenv("CARDO_CLICKHOUSE_USER"),
		Password: os.Getenv("CARDO_CLICKHOUSE_PASSWORD"),
		Timeout:  30 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// record describes one actor-day to seed.
type record struct {
	email    string
	sessions int
	accepted int
	rejected int
	cents    int
	model    string
	src      source.Name
	unknown  []string
}

// scrub builds a stored record the only way a store can receive one: through the hasher.
func scrub(t *testing.T, day time.Time, r record) pseudonym.Scrubbed {
	t.Helper()
	h, err := pseudonym.New(testSalt, "v1")
	if err != nil {
		t.Fatal(err)
	}
	extra := ""
	if len(r.unknown) > 0 {
		extra = `, "experimental_metric": 7`
	}
	model := r.model
	if model == "" {
		model = "claude-opus-5"
	}
	src := r.src
	if src == "" {
		src = testSource
	}
	raw := fmt.Sprintf(`{
		"actor": {"type": "user_actor", "email_address": %q},
		"organization_id": "org-test",
		"customer_type": "api",
		"terminal_type": "vscode",
		"core_metrics": {
			"num_sessions": %d,
			"lines_of_code": {"added": 100, "removed": 40},
			"commits_by_claude_code": 2,
			"pull_requests_by_claude_code": 1
		},
		"tool_actions": {
			"edit_tool": {"accepted": %d, "rejected": %d},
			"multi_edit_tool": {"accepted": 0, "rejected": 0},
			"write_tool": {"accepted": 3, "rejected": 1},
			"notebook_edit_tool": {"accepted": 0, "rejected": 0}
		},
		"model_breakdown": [{
			"model": %q,
			"tokens": {"input": 1000, "output": 500, "cache_read": 10, "cache_creation": 5},
			"estimated_cost": {"currency": "USD", "amount": %d}
		}]%s
	}`, r.email, r.sessions, r.accepted, r.rejected, model, r.cents, extra)

	s, err := h.Scrub(source.Record{
		Source: src, Day: day,
		ActorType: "user_actor", ActorID: r.email,
		ActorPath:    []string{"actor", "email_address"},
		OrgID:        "org-test",
		CustomerType: "api",
		TerminalType: "vscode",
		Raw:          json.RawMessage(raw),
		Unknown:      r.unknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// query runs a statement and returns the trimmed response body.
func query(t *testing.T, st *Store, q string) string {
	t.Helper()
	out, err := st.c.Query(context.Background(), q)
	if err != nil {
		t.Fatalf("query failed: %v\n  %s", err, q)
	}
	return strings.TrimSpace(string(out))
}

// put writes records for a day and registers cleanup of both partitions.
func put(t *testing.T, st *Store, day time.Time, records []record) string {
	t.Helper()
	key := putNoCleanup(t, st, testSource, day, records)
	t.Cleanup(func() {
		partition := fmt.Sprintf("('%s', '%s')", testSource, key)
		for _, tbl := range []string{BronzeTable, stagingTable} {
			_ = st.c.Exec(context.Background(),
				fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", tbl, partition))
		}
	})
	return key
}

// putNoCleanup writes a day and leaves it there, for the sample-data seeder.
func putNoCleanup(t *testing.T, st *Store, src source.Name, day time.Time, records []record) string {
	t.Helper()
	key := day.Format("2006-01-02")
	scrubbed := make([]pseudonym.Scrubbed, 0, len(records))
	for _, r := range records {
		r.src = src
		scrubbed = append(scrubbed, scrub(t, day, r))
	}
	if err := st.Put(context.Background(), src, key, scrubbed); err != nil {
		t.Fatal(err)
	}
	return key
}

// day returns a UTC day inside the retention window, so the TTL on bronze cannot delete the test's
// own rows out from under it.
func testDay(offset int) time.Time {
	return time.Now().UTC().AddDate(0, 0, -offset).Truncate(24 * time.Hour)
}

var threeActors = []record{
	{email: testEmail, sessions: 5, accepted: 45, rejected: 5, cents: 113},
	{email: "second.person@example-corp.com", sessions: 3, accepted: 20, rejected: 0, cents: 153},
	{email: "third.person@example-corp.com", sessions: 2, accepted: 10, rejected: 10, cents: 193,
		unknown: []string{"experimental_metric"}},
}

// The gold marts are what a dashboard reads, so they are what is asserted. Costs of 113, 153 and
// 193 cents total 459 cents and must surface as exactly 4.59 dollars -- the cents-to-dollars
// conversion is the one arithmetic error here that would be invisible on a chart and wrong by a
// factor of a hundred.
func TestGoldMartsOverRealClickHouse(t *testing.T) {
	st := testStore(t)
	d := testDay(3)
	key := put(t, st, d, threeActors)

	t.Run("fleet adoption", func(t *testing.T) {
		got := query(t, st, fmt.Sprintf(
			"SELECT active_actors, sessions, lines_added, commits FROM %s.gold_fleet_adoption "+
				"WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
			Database, testSource, key))
		if want := "3\t10\t300\t6"; got != want {
			t.Errorf("gold_fleet_adoption = %q, want %q", got, want)
		}
	})

	t.Run("cost is dollars not cents", func(t *testing.T) {
		got := query(t, st, fmt.Sprintf(
			"SELECT cost_usd FROM %s.gold_cost_daily WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
			Database, testSource, key))
		if got != "4.59" {
			t.Errorf("gold_cost_daily.cost_usd = %q, want %q. "+
				"459 cents must be 4.59 dollars; 459 here means a missing division, "+
				"0.0459 means a double one", got, "4.59")
		}
	})

	t.Run("acceptance rate is published with its denominator", func(t *testing.T) {
		// 75 accepted (45+20+10 edits, plus 3 writes each) against 16 rejected.
		got := query(t, st, fmt.Sprintf(
			"SELECT accepted, rejected, proposals, edit_proposals FROM %s.gold_tool_acceptance "+
				"WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
			Database, testSource, key))
		if want := "84\t18\t102\t90"; got != want {
			t.Errorf("gold_tool_acceptance = %q, want %q", got, want)
		}
	})

	t.Run("model mix", func(t *testing.T) {
		got := query(t, st, fmt.Sprintf(
			"SELECT model, actors, tokens_input, cost_usd, cost_share FROM %s.gold_model_mix "+
				"WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
			Database, testSource, key))
		if want := "claude-opus-5\t3\t3000\t4.59\t1"; got != want {
			t.Errorf("gold_model_mix = %q, want %q", got, want)
		}
	})

	t.Run("drift is visible as data", func(t *testing.T) {
		got := query(t, st, fmt.Sprintf(
			"SELECT unmodelled_field, rows_affected FROM %s.gold_schema_drift "+
				"WHERE source = '%s' FORMAT TabSeparated",
			Database, testSource))
		if want := "experimental_metric\t1"; got != want {
			t.Errorf("gold_schema_drift = %q, want %q", got, want)
		}
	})
}

// INV-2 -- user.email is never persisted. Behavioural, not static: the record goes through the
// real pipeline into a real ClickHouse, and then every String column of every row is searched for
// the address. A static check could be satisfied by renaming a variable; this one cannot.
func TestINV2_NoEmailReachesClickHouse(t *testing.T) {
	st := testStore(t)
	key := put(t, st, testDay(4), threeActors)

	rows := query(t, st, fmt.Sprintf(
		"SELECT count() FROM %s WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
		BronzeTable, testSource, key))
	if rows != "3" {
		t.Fatalf("expected 3 rows to scan, got %q -- this test would otherwise prove nothing", rows)
	}

	// Concatenate every string-typed column and look for the identifier and for the word itself.
	concat := "concat(source, pseudonym, salt_version, actor_type, org_id, customer_type, " +
		"terminal_type, payload, arrayStringConcat(unknown_fields, ','))"
	for _, needle := range []string{testEmail, "second.person", "@example-corp.com", "email"} {
		got := query(t, st, fmt.Sprintf(
			"SELECT count() FROM %s WHERE source = '%s' AND day = '%s' AND position(%s, '%s') > 0 "+
				"FORMAT TabSeparated",
			BronzeTable, testSource, key, concat, needle))
		if got != "0" {
			t.Errorf("INV-2 violated: %s rows in ClickHouse contain %q.\n"+
				"  Pseudonymization must happen before storage, always.", got, needle)
		}
	}
}

// Put replaces a day rather than appending. Both analytics APIs revise recent days, so a poller
// that appended would accumulate diverging copies and quietly double every total.
func TestPutReplacesTheDayRatherThanAppending(t *testing.T) {
	st := testStore(t)
	d := testDay(5)
	key := put(t, st, d, threeActors)

	// Re-poll the same day and get a different answer back: one actor, and a different cost.
	corrected := []record{{email: testEmail, sessions: 9, accepted: 1, rejected: 0, cents: 700}}
	if err := st.Put(context.Background(), testSource, key, []pseudonym.Scrubbed{
		scrub(t, d, corrected[0]),
	}); err != nil {
		t.Fatal(err)
	}

	got := query(t, st, fmt.Sprintf(
		"SELECT count(), sum(JSONExtractInt(payload, 'core_metrics', 'num_sessions')) FROM %s "+
			"WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
		BronzeTable, testSource, key))
	if want := "1\t9"; got != want {
		t.Errorf("after re-polling, bronze holds %q, want %q.\n"+
			"  10 sessions would mean the first write survived alongside the second, which is "+
			"the double-counting Put exists to prevent", got, want)
	}
}

// A day with no activity is a real answer and must overwrite what was there before -- otherwise a
// corrected day that lost all its rows keeps reporting the old ones forever.
func TestEmptyPutClearsTheDay(t *testing.T) {
	st := testStore(t)
	d := testDay(6)
	key := put(t, st, d, threeActors)

	if err := st.Put(context.Background(), testSource, key, nil); err != nil {
		t.Fatal(err)
	}
	got := query(t, st, fmt.Sprintf(
		"SELECT count() FROM %s WHERE source = '%s' AND day = '%s' FORMAT TabSeparated",
		BronzeTable, testSource, key))
	if got != "0" {
		t.Errorf("after an empty poll the day holds %q rows, want 0", got)
	}
}

// Migrations are applied on every run, so being safe to repeat is a requirement rather than a
// nicety. A second Migrate must be a no-op and must not re-apply anything.
func TestMigrationsAreIdempotent(t *testing.T) {
	st := testStore(t) // already migrated once
	before := query(t, st, fmt.Sprintf(
		"SELECT count() FROM %s.schema_migrations FINAL FORMAT TabSeparated", Database))

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := query(t, st, fmt.Sprintf(
		"SELECT count() FROM %s.schema_migrations FINAL FORMAT TabSeparated", Database))

	if before != after {
		t.Errorf("migration ledger grew from %q to %q on a repeat run", before, after)
	}
	if after == "0" {
		t.Error("no migrations were recorded; this test would pass vacuously")
	}
}

// The partition values are interpolated into DDL. They are internally generated today, which is
// exactly the assumption worth not relying on.
func TestPutRefusesMalformedPartitionValues(t *testing.T) {
	st := testStore(t)
	for _, tc := range []struct{ name, src, day string }{
		{"sql in day", "console", "2026-01-01') DROP TABLE x --"},
		{"sql in source", "console'; DROP TABLE x --", "2026-01-01"},
		{"nonsense day", "console", "yesterday"},
		{"impossible date", "console", "2026-13-45"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := st.Put(context.Background(), source.Name(tc.src), tc.day, nil)
			if err == nil {
				t.Fatal("expected a refusal, got nil")
			}
			if !strings.Contains(err.Error(), "refusing") {
				t.Errorf("expected a refusal, got: %v", err)
			}
		})
	}
}
