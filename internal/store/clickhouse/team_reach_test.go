package clickhouse

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests run against a real ClickHouse and are skipped unless CARDO_CLICKHOUSE_URL is set:
//
//	CARDO_CLICKHOUSE_URL=... CARDO_CLICKHOUSE_USER=... CARDO_CLICKHOUSE_PASSWORD=... \
//	  go test ./internal/store/clickhouse/ -run TestTeamReach
//
// The view under test reads the collector path: a session with a pseudonym, an InstructionsLoaded
// hook for the same session, and a team directory that maps people to teams.

func teamStore(t *testing.T) *Store {
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

// seedTeamReach writes one team's people, their sessions and their instruction loads, and returns
// the names it used so the test can clean up.
func seedTeamReach(t *testing.T, st *Store, team string, people int) (pseudos []string, batch string) {
	t.Helper()
	ctx := context.Background()
	stamp := fmt.Sprintf("teamreach-%d", time.Now().UnixNano())
	batch = "teamreach-" + stamp
	pseudos = make([]string, people)

	for i := 0; i < people; i++ {
		pseudos[i] = fmt.Sprintf("%064x", time.Now().UnixNano()+int64(i))
	}
	for i, p := range pseudos {
		session := fmt.Sprintf("%s-sess-%d", stamp, i)
		// A session with a pseudonym and a salt version, so the directory can join it.
		otel := fmt.Sprintf(
			"INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) " +
				"VALUES (toDateTime64('2026-10-20 10:00:00',9),'claude-code','','x'," +
				"{'cardo.salt_version':'v1','user.pseudonym':'" + p + "','session.id':'" + session + "'}," +
				"'','','',{}," +
				"{'session.id':'" + session + "','user.pseudonym':'" + p + "'}," +
				"'claude_code.user_prompt')")
		if err := st.c.Exec(ctx, otel); err != nil {
			t.Fatalf("cannot seed the session: %v", err)
		}
		// The instruction load for the same session, which is what the view counts.
		hook := fmt.Sprintf(
			"INSERT INTO cardo.bronze_hook_events (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) " +
				"VALUES (toDateTime64('2026-10-20 10:00:00',9),'claude-code','','x',{},'','','',{}," +
				"{'session_id':'" + session + "','prompt_id':'p" + strconv.Itoa(i) + "','instructions_name':'acme-rules-v1.md','instructions_file':'CLAUDE.md','load_reason':'project'}," +
				"'InstructionsLoaded')")
		if err := st.c.Exec(ctx, hook); err != nil {
			t.Fatalf("cannot seed the instruction load: %v", err)
		}
		// The directory row: this person is in this team.
		row := fmt.Sprintf("INSERT INTO cardo.settings_teams (batch, team, pseudonym, salt_version) VALUES ('%s','%s','%s','v1')", batch, team, p)
		if err := st.c.Exec(ctx, row); err != nil {
			t.Fatalf("cannot seed the directory: %v", err)
		}
	}
	if err := st.c.Exec(ctx, fmt.Sprintf(
		"INSERT INTO cardo.settings_teams_batches (batch, source_hash, rows, teams) VALUES ('%s','testhash',%d,1)", batch, people)); err != nil {
		t.Fatalf("cannot seed the batch marker: %v", err)
	}
	t.Cleanup(func() {
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.settings_teams DELETE WHERE batch = '%s' SETTINGS mutations_sync = 1", batch))
		_ = st.c.Exec(ctx, fmt.Sprintf("ALTER TABLE cardo.settings_teams_batches DELETE WHERE batch = '%s' SETTINGS mutations_sync = 1", batch))
		for _, p := range pseudos {
			_ = st.c.Exec(ctx, fmt.Sprintf(
				"ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['user.pseudonym'] = '%s' SETTINGS mutations_sync = 1", p))
			_ = st.c.Exec(ctx, fmt.Sprintf(
				"ALTER TABLE cardo.bronze_hook_events DELETE WHERE LogAttributes['session_id'] LIKE '%s-sess-%%' SETTINGS mutations_sync = 1", stamp))
		}
	})
	return pseudos, batch
}

// A team's reach is the artifact's spread inside that team, with the organization as the
// denominator. This is the number ADR-0030 asks for: teams compared on reach, never ranked.
func TestTeamReachCountsTheTeamsPeople(t *testing.T) {
	st := teamStore(t)
	_, _ = seedTeamReach(t, st, "reach-team-alpha", 6)

	got := query(t, st, fmt.Sprintf(
		"SELECT people, uses FROM cardo.gold_team_reach WHERE team = 'reach-team-alpha' AND artifact = 'acme-rules-v1.md' FORMAT TabSeparated"))
	if want := "6\t6"; got != want {
		t.Errorf("gold_team_reach = %q, want %q", got, want)
	}
}

// A team below the group floor is not rendered at all: "1 person" is a person (ADR-0046).
func TestATeamBelowTheFloorIsNotRendered(t *testing.T) {
	st := teamStore(t)
	_, _ = seedTeamReach(t, st, "reach-team-tiny", 2)

	got := query(t, st, "SELECT count() FROM cardo.gold_team_reach WHERE team = 'reach-team-tiny' FORMAT TabSeparated")
	if want := "0"; got != want {
		t.Errorf("a team of two people is rendered %s time(s), want 0", got)
	}
}

// The view names no pseudonym. It publishes a team's reach, never a person's (INV-3).
func TestTeamReachNamesNoPseudonym(t *testing.T) {
	st := teamStore(t)
	pseudos, _ := seedTeamReach(t, st, "reach-team-gamma", 6)

	got := query(t, st, "SELECT count() FROM cardo.gold_team_reach WHERE team = 'reach-team-gamma' FORMAT TabSeparated")
	if got == "0" {
		t.Fatal("the team is not rendered at all, so the check below proves nothing")
	}
	cols := query(t, st, "SELECT name FROM system.columns WHERE database = 'cardo' AND table = 'gold_team_reach' FORMAT TabSeparated")
	for _, p := range pseudos {
		if strings.Contains(cols, p) {
			t.Fatal("a pseudonym appears in the view's columns")
		}
	}
}

// The organization is the denominator for every team alike, which is what keeps the comparison
// from being a league table.
func TestTeamReachCarriesTheFleetAsTheDenominator(t *testing.T) {
	st := teamStore(t)
	_, _ = seedTeamReach(t, st, "reach-team-delta", 6)

	got := query(t, st, fmt.Sprintf(
		"SELECT fleet_people > 0 FROM cardo.gold_team_reach WHERE team = 'reach-team-delta' AND artifact = 'acme-rules-v1.md' FORMAT TabSeparated"))
	if want := "1"; got != want {
		t.Errorf("the fleet denominator is %q, want %q", got, want)
	}
}
