package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// goldEnv stands in for ClickHouse and enforces the boundary the way grants do: the admin user may
// do anything, the read-only user only reads the leadership database.
type goldEnv struct {
	mu     sync.Mutex
	leaked bool // set to simulate a missing grant
	calls  int
}

func (g *goldEnv) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.calls++
		leaked := g.leaked
		g.mu.Unlock()
		user := r.Header.Get("X-ClickHouse-User")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		sql := strings.ToLower(string(body))
		if user == "cardo_ro" {
			if leaked || strings.Contains(sql, "leadership.") {
				_, _ = w.Write([]byte("1\n"))
				return
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Code: 497. Not enough privileges"))
			return
		}
		switch {
		case strings.Contains(sql, "system.tables"):
			_, _ = w.Write([]byte("gold_artifact_usage\ngold_fleet_adoption\n"))
		case strings.HasPrefix(strings.TrimSpace(sql), "select count"):
			_, _ = w.Write([]byte("0\n"))
		default:
			_, _ = w.Write([]byte(""))
		}
	}
}

func goldOptions(t *testing.T, g *goldEnv) Options {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "pw", "GRAFANA_RO_PASSWORD": strings.Repeat("cd", 32)}
	if err := WriteDotEnv(dir+"/.env", env, Secrets{Salt: strings.Repeat("ab", 32), ClickHousePassword: "pw", GrafanaPassword: "gr"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(g.handler())
	t.Cleanup(srv.Close)
	return Options{Dir: dir, URLFn: func(port, path string) string { return srv.URL + path }}
}

func TestGoldAccessCreatesTheUserAndProvesTheRefusal(t *testing.T) {
	g := &goldEnv{}
	res, err := RunStep(context.Background(), goldOptions(t, g), "gold")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("gold step failed: %s / %s", res.Cause, res.Fix)
	}
	if !strings.Contains(res.Value, "bronze and silver refused") {
		t.Errorf("value = %q, want it to state the refusal", res.Value)
	}
}

func TestGoldAccessFailsWhenTheReadOnlyUserCanSeeBronze(t *testing.T) {
	g := &goldEnv{leaked: true}
	res, err := RunStep(context.Background(), goldOptions(t, g), "gold")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("gold step passed while the read-only user could read bronze")
	}
	if !strings.Contains(res.Cause, "bronze") {
		t.Errorf("cause = %q, want it to name the leaked table", res.Cause)
	}
}

func TestCheckGoldAccessWaitsForTheUser(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDotEnv(dir+"/.env", map[string]string{"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "pw"},
		Secrets{Salt: strings.Repeat("ab", 32), ClickHousePassword: "pw", GrafanaPassword: "gr"}); err != nil {
		t.Fatal(err)
	}
	res, err := checkGoldAccess(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pending || res.OK {
		t.Fatalf("check = %+v, want pending until cardo gold runs", res)
	}
}
