package launcher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// teamsStub answers teamsView's queries like a store with no directory loaded but sessions that
// carry cohort labels: two labels, one of them a single person (below the floor).
func teamsStub(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := WriteDotEnv(dir+"/.env", map[string]string{
		"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "pw",
	}, Secrets{}); err != nil {
		t.Fatal(err)
	}
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		switch {
		case strings.Contains(q, "settings_effective"):
			fmt.Fprint(w, "5\n")
		case strings.Contains(q, "settings_teams_batches"):
			// No directory loaded: no batch rows.
		case strings.Contains(q, "GROUP BY cohort"):
			fmt.Fprint(w, `{"team":"platform","people":6,"sessions":7}`+"\n"+
				`{"team":"solo","people":1,"sessions":2}`+"\n")
		case strings.Contains(q, "uniqExact(pseudonym)") && strings.Contains(q, "cohort = ''"):
			fmt.Fprint(w, "2\n")
		case strings.Contains(q, "cohort != ''"):
			fmt.Fprint(w, "8\n")
		case strings.Contains(q, "count() FROM cardo.silver_session_live"):
			fmt.Fprint(w, "10\n")
		}
	}))
	t.Cleanup(ch.Close)
	s, err := NewServer(Options{Dir: dir, Out: &bytes.Buffer{}, Version: "test",
		URLFn: func(port, path string) string { return ch.URL + path }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Teams must start without a directory: the cohort labels sessions already carry are a team's
// name until the real thing is loaded, and the floor hides a label of one person -- a team of two
// people is a person.
func TestTeamsStartFromCohortLabelsWithoutADirectory(t *testing.T) {
	s := teamsStub(t)
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
	got := v.TeamRows[0]
	if got.Team != "platform" || got.People != 6 || got.Sessions != 7 {
		t.Fatalf("team row = %+v, want platform 6 people 7 sessions", got)
	}
	if v.HiddenTeams != 1 {
		t.Fatalf("hidden teams = %d, want 1 (the label of one person)", v.HiddenTeams)
	}
	if v.Unassigned != 2 || v.Mapped != 8 {
		t.Fatalf("unassigned = %d mapped = %d, want 2 and 8", v.Unassigned, v.Mapped)
	}
	if v.CoveragePct != 80 {
		t.Fatalf("coverage = %v, want 80 (8 of 10 sessions labelled)", v.CoveragePct)
	}
}
