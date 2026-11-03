package launcher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// teamsStub answers teamsView's queries like a store whose sessions already have teams: two of
// them, one a single person (below the floor), and ten people in all, eight of them in a team.
// With a directory, the store also has a loaded batch.
func teamsStub(t *testing.T, directory bool) (*Server, func() []string) {
	t.Helper()
	dir := t.TempDir()
	if err := WriteDotEnv(dir+"/.env", map[string]string{
		"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "pw",
	}, Secrets{}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []string
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		mu.Lock()
		seen = append(seen, q)
		mu.Unlock()
		switch {
		case strings.Contains(q, "settings_effective"):
			fmt.Fprint(w, "5\n")
		case strings.Contains(q, "settings_teams_batches"):
			if directory {
				fmt.Fprint(w, `{"batch":"b1","at":"2026-11-01 10:00:00","rows":12,"teams":3,"source_hash":"h"}`+"\n")
			}
		case strings.Contains(q, "uniqExactIf"):
			fmt.Fprint(w, `{"people":10,"placed":8}`+"\n")
		case strings.Contains(q, "GROUP BY t.team"):
			fmt.Fprint(w, `{"team":"platform","people":7,"sessions":9}`+"\n"+
				`{"team":"solo","people":1,"sessions":2}`+"\n")
		}
	}))
	t.Cleanup(ch.Close)
	s, err := NewServer(Options{Dir: dir, Out: &bytes.Buffer{}, Version: "test",
		URLFn: func(port, path string) string { return ch.URL + path }})
	if err != nil {
		t.Fatal(err)
	}
	return s, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

// Teams start without a directory, from the labels sessions carry, and every figure counts people.
// The floor hides a team of one person: a team that small is a person.
func TestTeamsStartFromCohortLabelsWithoutADirectory(t *testing.T) {
	s, _ := teamsStub(t, false)
	v, err := s.teamsView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Loaded || v.Source != "labels" {
		t.Fatalf("source = %q loaded = %v, want labels with no directory", v.Source, v.Loaded)
	}
	if len(v.TeamRows) != 1 {
		t.Fatalf("team rows = %v, want the one team at or above the floor", v.TeamRows)
	}
	if got := v.TeamRows[0]; got.Team != "platform" || got.People != 7 || got.Sessions != 9 {
		t.Fatalf("team row = %+v, want platform, 7 people, 9 sessions", got)
	}
	if v.HiddenTeams != 1 {
		t.Fatalf("hidden teams = %d, want 1 (the team of one person)", v.HiddenTeams)
	}
	if v.People7d != 10 || v.Mapped != 8 || v.Unassigned != 2 || v.CoveragePct != 80 {
		t.Fatalf("people = %d, in a team = %d, without = %d, share = %v; want 10, 8, 2 and 80",
			v.People7d, v.Mapped, v.Unassigned, v.CoveragePct)
	}
}

// The page has no team rule of its own. It reads silver_session_team, the one place that says
// which team a session is in, so a person cannot sit in one team here and another on a dashboard.
// And "unassigned" -- what that view writes for no team, and the label the shipped settings bundle
// gives every session -- is never shown as a team. With or without a directory, the queries are
// the same.
func TestTeamsReadTheOneTeamRuleAndUnassignedIsNoTeam(t *testing.T) {
	for _, directory := range []bool{false, true} {
		s, seen := teamsStub(t, directory)
		v, err := s.teamsView(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if directory && (!v.Loaded || v.Source != "directory") {
			t.Fatalf("with a directory: source = %q loaded = %v, want directory", v.Source, v.Loaded)
		}
		counted := 0
		for _, q := range seen() {
			if strings.Contains(q, "cohort") {
				t.Errorf("a query reads the session label itself, beside the shared rule: %s", q)
			}
			if !strings.Contains(q, "pseudonym") {
				continue
			}
			counted++
			if !strings.Contains(q, "cardo.silver_session_team") {
				t.Errorf("a query counts people without reading silver_session_team: %s", q)
			}
			if !strings.Contains(q, "t.team != 'unassigned'") {
				t.Errorf("a query counts people without setting \"unassigned\" apart: %s", q)
			}
		}
		if counted != 2 {
			t.Errorf("directory=%v: %d queries counted people, want the totals and the team list", directory, counted)
		}
	}
}
