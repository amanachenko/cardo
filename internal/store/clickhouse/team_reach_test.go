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

// teamReachSeed is one team's week: how many people had a session, how many of them the directory
// lists under the team, how many loaded the instruction file, and what label the sessions carry.
type teamReachSeed struct {
	team        string
	active      int
	inDirectory int
	loads       int
	cohort      string
}

// seedTeamReach writes one team's people, their sessions and their instruction loads, and returns
// the names it used so the test can clean up.
func seedTeamReach(t *testing.T, st *Store, team string, people int) (pseudos []string, batch string) {
	t.Helper()
	return seedTeamWeek(t, st, teamReachSeed{team: team, active: people, inDirectory: people, loads: people})
}

// seedTeamWeek writes the week for one or more teams in a single directory batch: `active` people
// per team with a session each, `loads` of them loading the organization's instruction file, and
// `inDirectory` of them listed in the directory under the team. The rest are not in the directory,
// and their sessions fall back to the label they carry (ADR-0034 decision 3).
//
// One batch for the whole test because the mapping is replaced by its newest batch: two batches in
// one test would leave the first team's directory rows out of the current one.
//
// The views read stored silver, so the rows are rebuilt before the helper returns and again after
// the clean-up deletes them (see .claude/rules/views.md, "Writing rows in a test and reading
// silver or gold straight after").
func seedTeamWeek(t *testing.T, st *Store, seeds ...teamReachSeed) (pseudos []string, batch string) {
	t.Helper()
	ctx := context.Background()
	stamp := fmt.Sprintf("teamreach-%d", time.Now().UnixNano())
	batch = "teamreach-" + stamp
	inDirectory := 0

	for si, seed := range seeds {
		cohort := ""
		if seed.cohort != "" {
			cohort = fmt.Sprintf(",'cardo.cohort':'%s'", seed.cohort)
		}
		for i := 0; i < seed.active; i++ {
			p := fmt.Sprintf("%064x", time.Now().UnixNano()+int64(si*1000003+i*7919))
			pseudos = append(pseudos, p)
			session := fmt.Sprintf("%s-sess-%d-%d", stamp, si, i)
			// A session with a pseudonym and a salt version, so the directory can join it.
			otel := fmt.Sprintf(
				"INSERT INTO cardo.bronze_otel_logs (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) " +
					"VALUES (toDateTime64('2026-10-20 10:00:00',9),'claude-code','','x'," +
					"{'cardo.salt_version':'v1','user.pseudonym':'" + p + "'" + cohort + "}," +
					"'','','',{}," +
					"{'session.id':'" + session + "','user.pseudonym':'" + p + "'}," +
					"'claude_code.user_prompt')")
			if err := st.c.Exec(ctx, otel); err != nil {
				t.Fatalf("cannot seed the session: %v", err)
			}
			// The instruction load for the first `loads` people, which is what the view counts.
			if i < seed.loads {
				hook := fmt.Sprintf(
					"INSERT INTO cardo.bronze_hook_events (Timestamp, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) " +
						"VALUES (toDateTime64('2026-10-20 10:00:00',9),'claude-code','','x',{},'','','',{}," +
						"{'session_id':'" + session + "','prompt_id':'p" + strconv.Itoa(i) + "','instructions_name':'acme-rules-v1.md','instructions_file':'CLAUDE.md','load_reason':'project'}," +
						"'InstructionsLoaded')")
				if err := st.c.Exec(ctx, hook); err != nil {
					t.Fatalf("cannot seed the instruction load: %v", err)
				}
			}
			// The directory row for the first `inDirectory` people.
			if i < seed.inDirectory {
				row := fmt.Sprintf("INSERT INTO cardo.settings_teams (batch, team, pseudonym, salt_version) VALUES ('%s','%s','%s','v1')", batch, seed.team, p)
				if err := st.c.Exec(ctx, row); err != nil {
					t.Fatalf("cannot seed the directory: %v", err)
				}
			}
		}
		inDirectory += seed.inDirectory
	}
	if err := st.c.Exec(ctx, fmt.Sprintf(
		"INSERT INTO cardo.settings_teams_batches (batch, source_hash, rows, teams) VALUES ('%s','testhash',%d,%d)", batch, inDirectory, len(seeds))); err != nil {
		t.Fatalf("cannot seed the batch marker: %v", err)
	}
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
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
		// The stored tables hold what the last rebuild saw, so the rows are rebuilt away too.
		if err := RebuildSilver(ctx, st.c); err != nil {
			t.Errorf("cannot rebuild silver after the clean-up: %v", err)
		}
	})
	return pseudos, batch
}

// A team's reach is the share of the team's own active people who used the artifact, with the
// organization's own share beside it. This is the number ADR-0030 asks for: teams compared on
// reach, never ranked.
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

// The floor counts the team's active people that week, not everyone the directory lists. A team
// of six where one person had a session is one person's week, and a leader can read every gold
// view -- so the team is not rendered at all. The premise is read in the same test, so an empty
// result cannot pass as a fix.
func TestTheFloorCountsTheTeamsActivePeopleNotTheDirectory(t *testing.T) {
	st := teamStore(t)
	_, _ = seedTeamWeek(t, st, teamReachSeed{team: "reach-team-listed", active: 1, inDirectory: 6, loads: 1})

	active := query(t, st, "SELECT count() FROM cardo.silver_session_team WHERE team = 'reach-team-listed' FORMAT TabSeparated")
	if strings.TrimSpace(active) != "1" {
		t.Fatalf("the premise is wrong: %s active sessions, want 1", active)
	}
	got := query(t, st, "SELECT count() FROM cardo.gold_team_reach WHERE team = 'reach-team-listed' FORMAT TabSeparated")
	if want := "0"; got != want {
		t.Errorf("a team of six listed people with one active is rendered %s time(s), want 0", got)
	}
}

// Reach is the share inside the team, beside the organization's own share. Against the
// organization as denominator, a team of five where everyone uses something reads as half of a
// team of fifty where ten do -- which is backwards. Both rows carry the same organization share,
// because it is the same week and the same artifact.
func TestTeamReachIsTheShareInsideTheTeam(t *testing.T) {
	st := teamStore(t)
	_, _ = seedTeamWeek(t, st,
		teamReachSeed{team: "reach-team-five", active: 5, inDirectory: 5, loads: 5},
		teamReachSeed{team: "reach-team-fifty", active: 50, inDirectory: 50, loads: 10})

	got := query(t, st, "SELECT team_reach, team_active_people FROM cardo.gold_team_reach "+
		"WHERE team = 'reach-team-five' AND artifact = 'acme-rules-v1.md' FORMAT TabSeparated")
	if want := "1\t5"; got != want {
		t.Errorf("five of five = %q, want %q", got, want)
	}
	got = query(t, st, "SELECT team_reach, team_active_people FROM cardo.gold_team_reach "+
		"WHERE team = 'reach-team-fifty' AND artifact = 'acme-rules-v1.md' FORMAT TabSeparated")
	if want := "0.2\t50"; got != want {
		t.Errorf("ten of fifty = %q, want %q", got, want)
	}
	got = query(t, st, "SELECT uniqExact(org_reach) = 1 AND min(org_reach) > 0 FROM cardo.gold_team_reach "+
		"WHERE artifact = 'acme-rules-v1.md' AND team IN ('reach-team-five','reach-team-fifty') FORMAT TabSeparated")
	if want := "1"; got != want {
		t.Errorf("the two rows' organization shares are not one positive number: %q", got)
	}
}

// One meaning of "team" (ADR-0034 decision 3): the directory's team when the person is listed,
// the session's own label otherwise. Without one rule a person sits in two teams on two pages.
func TestOneMeaningOfTeam(t *testing.T) {
	st := teamStore(t)
	// Five people the directory lists under payments, whose sessions carry the search label, and
	// five people not in the directory, whose sessions fall back to their own label.
	_, _ = seedTeamWeek(t, st,
		teamReachSeed{team: "reach-payments", active: 5, inDirectory: 5, loads: 5, cohort: "search"},
		teamReachSeed{team: "", active: 5, inDirectory: 0, loads: 5, cohort: "search"})

	got := query(t, st, "SELECT team, people FROM cardo.gold_team_reach "+
		"WHERE artifact = 'acme-rules-v1.md' AND team IN ('reach-payments','search') ORDER BY team FORMAT TabSeparated")
	if want := "reach-payments\t5\nsearch\t5"; got != want {
		t.Errorf("the two shapes land in %q, want %q", got, want)
	}
}
