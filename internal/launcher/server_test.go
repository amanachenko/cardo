package launcher

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *fakeDeps) {
	t.Helper()
	f := &fakeDeps{}
	s, err := NewServer(Options{Dir: t.TempDir(), Out: &bytes.Buffer{}, Deps: f.deps()})
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func TestStateServesEveryStepGrouped(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got stateView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != len(Steps) {
		t.Errorf("total = %d, want %d", got.Total, len(Steps))
	}
	if len(got.Groups) < 3 {
		t.Errorf("groups = %d, want at least 3", len(got.Groups))
	}
	for _, g := range got.Groups {
		if len(g.Steps) == 0 {
			t.Errorf("group %q is empty", g.Name)
		}
		for _, st := range g.Steps {
			if st.State != "wait" && st.State != "planned" {
				t.Errorf("%s state = %q, want wait or planned on a fresh board", st.ID, st.State)
			}
			if st.Planned && st.State != "planned" {
				t.Errorf("%s is marked planned but reports %q", st.ID, st.State)
			}
			if st.Title == "" || st.Action == "" {
				t.Errorf("%s has no title or action: %+v", st.ID, st)
			}
		}
	}
}

func TestRunWithoutTokenIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"docker"}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: any page in the browser can reach loopback", rec.Code)
	}
}

func TestRunExecutesTheStepAndRecordsIt(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"docker"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, _ := marshalState(t, s)
		for _, g := range state.Groups {
			for _, st := range g.Steps {
				if st.ID == "docker" && st.State == "ok" {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("docker never reached ok; the button did not run the step")
}

func TestBusyStepIsConflict(t *testing.T) {
	s, _ := newTestServer(t)
	s.running["docker"] = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"docker"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestUnknownStepIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"deploy-everything"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// The board imports a copy of the stylesheet, because go:embed cannot reach outside the package.
// This test is the guard against the two drifting apart.
func TestEmbeddedStylesheetMatchesTheDesignFile(t *testing.T) {
	embedded, err := webFS.ReadFile("web/cardo-ui.css")
	if err != nil {
		t.Fatal(err)
	}
	design, err := os.ReadFile(filepath.Join("..", "..", "docs", "design", "cardo-ui.css"))
	if err != nil {
		t.Fatalf("read design stylesheet: %v", err)
	}
	if !bytes.Equal(embedded, design) {
		t.Fatal("internal/launcher/web/cardo-ui.css has drifted from docs/design/cardo-ui.css: copy the design file over it")
	}
}

func marshalState(t *testing.T, s *Server) (stateView, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	var out stateView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out, rec.Body.Bytes()
}

func TestStateWithoutTokenIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: the board exposes what the CLI found", rec.Code)
	}
}

func TestRootTradesTheTokenForACookie(t *testing.T) {
	s, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?t="+s.Token(), nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "cardo_session" || !cookies[0].HttpOnly {
		t.Fatalf("cookies = %v, want one HttpOnly cardo_session", cookies)
	}

	// The cookie alone is enough afterwards, so a reload keeps working after the token leaves the URL.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.AddCookie(cookies[0])
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d with the cookie, want 200", rec.Code)
	}
}

func TestStaleTokenIsRefusedWithInstructions(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?t=stale", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cardo ui") {
		t.Errorf("body = %q, want it to name the command that prints a new link", rec.Body.String())
	}
}

func TestCookieNameIsScopedToThePort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "127.0.0.1:8488"
	if got := cookieName(r); got != "cardo_session_8488" {
		t.Errorf("cookieName = %q, want cardo_session_8488", got)
	}
}

// "Run checks" must not restart the stack or rewrite secrets.
func TestCheckRunStartsOnlyReadOnlySteps(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"step":"check"}`))
	req.Header.Set("X-Cardo-Token", s.Token())
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Started []string `json:"started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "clickhouse", "grafana", "collector"}
	if len(body.Started) != len(want) {
		t.Fatalf("started = %v, want %v", body.Started, want)
	}
	for i := range want {
		if body.Started[i] != want[i] {
			t.Fatalf("started = %v, want %v", body.Started, want)
		}
	}
	for _, id := range body.Started {
		if id == "secrets" || id == "stack" {
			t.Fatalf("check ran a mutating step: %s", id)
		}
	}
}

// The organization card reads state through one endpoint; the key is never returned.
func TestAdminStateNeverReturnsTheKey(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	req.Header.Set("X-Cardo-Token", s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "key") && strings.Contains(rec.Body.String(), "sk-ant") {
		t.Fatalf("state leaked a key: %s", rec.Body.String())
	}
	var v adminView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Configured {
		t.Error("configured = true without a key")
	}
}

func TestAdminSaveRejectsAShortIntervalBeforeWriting(t *testing.T) {
	s, _ := newTestServer(t)
	body := strings.NewReader(`{"key":"sk-ant-admin-x","interval":"5m"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin", body)
	req.Header.Set("X-Cardo-Token", s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(s.opts.Dir, ".env")); !os.IsNotExist(err) {
		t.Error(".env was written for a rejected interval")
	}
}
