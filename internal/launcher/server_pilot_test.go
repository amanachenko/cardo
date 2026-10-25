package launcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// pilotEnv stands in for ClickHouse and answers the one question the launcher asks about the
// pilot: whether the organization chose the team path.
type pilotEnv struct {
	mu    sync.Mutex
	pilot bool
}

func (p *pilotEnv) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		sql := string(body)
		p.mu.Lock()
		pilot := p.pilot
		p.mu.Unlock()
		if strings.Contains(sql, "pilot_mode") {
			if pilot {
				_, _ = w.Write([]byte("1\n"))
			} else {
				_, _ = w.Write([]byte("0\n"))
			}
			return
		}
		_, _ = w.Write([]byte("0\n"))
	}
}

// pilotServer builds a board whose store says whether this is a pilot.
func pilotServer(t *testing.T, pilot bool) (*Server, *pilotEnv) {
	t.Helper()
	p := &pilotEnv{pilot: pilot}
	dir := t.TempDir()
	if err := WriteDotEnv(dir+"/.env", map[string]string{"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "pw"},
		Secrets{Salt: strings.Repeat("ab", 32), ClickHousePassword: "pw", GrafanaPassword: "gr"}); err != nil {
		t.Fatal(err)
	}
	store := httptest.NewServer(p.handler())
	t.Cleanup(store.Close)
	s, err := NewServer(Options{Dir: dir, Out: &strings.Builder{}, URLFn: func(port, path string) string { return store.URL + path }})
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

// "Install all" on a solo install must not run a team row. The pilot rows write a bundle for the
// machines an organization manages and a notice for its engineers; a single person has neither to
// hand, and the solo path must not create them behind the operator's back.
func TestInstallAllOnSoloRunsNoTeamRow(t *testing.T) {
	s, _ := pilotServer(t, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"all"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	started := map[string]bool{}
	for _, id := range startedIDs(t, rec) {
		started[id] = true
	}
	for _, m := range Steps {
		if m.Pilot && started[m.ID] {
			t.Errorf("the solo install ran the team row %q", m.ID)
		}
	}
}

// A pilot install runs the team rows. The organization chose that path, so the leaders' login,
// the bundle and the notice are what it asked for.
func TestInstallAllOnAPilotRunsTheTeamRows(t *testing.T) {
	s, _ := pilotServer(t, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"all"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	started := map[string]bool{}
	for _, id := range startedIDs(t, rec) {
		started[id] = true
	}
	for _, want := range []string{"gold", "bundle", "notice"} {
		if !started[want] {
			t.Errorf("the pilot install did not run the %q row", want)
		}
	}
}

// The notice a pilot writes names the security team as the salt's holder, because the board's path
// selector chose the team path. The step reads that choice from the settings table, where the
// board writes it.
func TestTheNoticeFromTheBoardNamesTheSecurityTeamInAPilot(t *testing.T) {
	s, _ := pilotServer(t, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"notice"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	waitFor(t, func() bool {
		b, err := os.ReadFile(filepath.Join(s.opts.Dir, "engineer-notice.md"))
		return err == nil && strings.Contains(string(b), "the security team")
	})
}

// startedIDs reads the steps the board says it began. The reply is the list, so a test does not
// have to watch a step's side effects to know it ran.
func startedIDs(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Started []string `json:"started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the reply is not JSON: %v", err)
	}
	return body.Started
}

// waitFor waits for a condition the board reaches on its own goroutine. A step runs in the
// background, so the test cannot expect the file to be there the moment the reply arrives.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the condition did not become true in five seconds")
}
