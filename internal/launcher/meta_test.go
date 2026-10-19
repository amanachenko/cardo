package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every command the board shows must be one a person can paste, and a placeholder must be impossible
// to copy by accident.
func TestStepCommandsAreExecutable(t *testing.T) {
	allowed := []string{"cardo ", "docker ", "lsof ", "nc ", "curl ", "powershell "}
	for _, m := range Steps {
		if m.Planned {
			continue
		}
		if strings.TrimSpace(m.Cmd) == "" {
			t.Errorf("%s: no command", m.ID)
			continue
		}
		ok := false
		for _, p := range allowed {
			if strings.HasPrefix(m.Cmd, p) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s: %q does not start with an executable command", m.ID, m.Cmd)
		}
		if strings.Contains(m.Cmd, "<") && m.Input == "" {
			t.Errorf("%s: %q contains a placeholder but the step takes no input", m.ID, m.Cmd)
		}
	}
	for _, id := range []string{"ports", "collector"} {
		var found bool
		for _, m := range Steps {
			if m.ID == id {
				found = m.Commands["windows"] != ""
			}
		}
		if !found {
			t.Errorf("%s: no Windows command; the Unix tool is not on Windows", id)
		}
	}
}

// Credentials are shown once in the run event and must never come back from /api/state.
func TestSecretsAreNotExposedByTheStateEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir)
	srv, err := NewServer(Options{Dir: dir, Out: os.Stdout})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()

	run := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(body))
		req.Header.Set("X-Cardo-Token", srv.Token())
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := run(`{"step":"secrets"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d: %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		req.Header.Set("X-Cardo-Token", srv.Token())
		handler.ServeHTTP(rec, req)
		var out stateView
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		done := false
		for _, g := range out.Groups {
			for _, s := range g.Steps {
				if s.ID == "secrets" && s.State == "ok" {
					done = true
				}
			}
		}
		if done {
			env, err := ReadDotEnv(filepath.Join(dir, ".env"))
			if err != nil {
				t.Fatal(err)
			}
			pw := env["GRAFANA_PASSWORD"]
			if pw == "" {
				t.Fatal("no password was generated")
			}
			if strings.Contains(rec.Body.String(), pw) {
				t.Fatal("/api/state contains the Grafana password")
			}
			if strings.Contains(rec.Body.String(), "reveal") {
				t.Fatal("/api/state carries a reveal field")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the secrets step did not finish")
}

// The reveal itself is present only in the run result the server stores momentarily.
func TestSecretsResultCarriesReveal(t *testing.T) {
	dir := t.TempDir()
	res, err := RunStep(context.Background(), Options{Dir: dir, Deps: (&fakeDeps{}).deps()}, "secrets")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reveal) == 0 {
		t.Fatal("a freshly generated .env must reveal its credentials once")
	}
	env, err := ReadDotEnv(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Reveal["Grafana password"] != env["GRAFANA_PASSWORD"] {
		t.Error("revealed password does not match .env")
	}
	if _, err := url.Parse("http://127.0.0.1:3001"); err != nil {
		t.Fatal(err)
	}
}

// The Admin API path is optional: the compose profile exists only when a key is configured.
func TestStackStartsThePollerOnlyWithAKey(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir)
	secrets := Secrets{Salt: strings.Repeat("a", 64), ClickHousePassword: "ch", GrafanaPassword: "gr"}

	run := func(extra map[string]string) []string {
		env := map[string]string{"CARDO_SALT": secrets.Salt, "CLICKHOUSE_PASSWORD": "ch", "GRAFANA_PASSWORD": "gr"}
		for k, v := range extra {
			env[k] = v
		}
		if err := WriteDotEnv(dir+"/.env", env, secrets); err != nil {
			t.Fatal(err)
		}
		f := &fakeDeps{}
		if _, err := RunStep(context.Background(), Options{Dir: dir, Deps: f.deps()}, "stack"); err != nil {
			t.Fatal(err)
		}
		return f.lastArgs("docker")
	}

	plain := strings.Join(run(nil), " ")
	if strings.Contains(plain, "--profile") {
		t.Errorf("stack without a key used a profile: %s", plain)
	}
	admin := strings.Join(run(map[string]string{"CARDO_ADMIN_KEY": "sk-ant-admin-x"}), " ")
	if !strings.Contains(admin, "--profile admin") {
		t.Errorf("stack with a key did not start the poller: %s", admin)
	}
}
