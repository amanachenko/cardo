package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/launcher"
	"github.com/amanachenko/cardo/internal/poll"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/source/console"
	"github.com/amanachenko/cardo/internal/store/clickhouse"
)

// TestE2EAdminPath runs the Admin API path end to end against the live ClickHouse in the reference
// stack: a stand-in API serving one realistic response, the real poller, the real store, and the
// board's own status endpoint. It is opt-in because it needs the stack.
//
//	CARDO_E2E=1 go test ./test -run TestE2EAdminPath -v
//
// The rows are tagged with a test organization id and deleted on the way out, so the fleet views are
// never left with a fake organization.
func TestE2EAdminPath(t *testing.T) {
	if os.Getenv("CARDO_E2E") == "" {
		t.Skip("set CARDO_E2E=1 and run the reference stack to execute this test")
	}
	user, pass := clickhouseCredentials(t)
	endpoint := "http://127.0.0.1:8124"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const orgID = "e2e-admin"
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)

	raw := readFixture(t, "fixtures/console/single-page.json")
	body := retag(t, raw, orgID, day)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Errorf("the poller sent no x-api-key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	if os.Getenv("CARDO_E2E_KEEP") == "" {
		defer cleanupOrg(t, endpoint, user, pass, orgID)
	} else {
		t.Logf("CARDO_E2E_KEEP set: rows for %s are left in place", orgID)
	}

	hasher, err := pseudonym.New(strings.Repeat("ab", 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError}))
	adapter, err := console.New("sk-ant-e2e", log, console.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	st, err := clickhouse.New(clickhouse.Config{Endpoint: endpoint, User: user, Password: pass, Timeout: 10 * time.Second}, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	runner := &poll.Runner{Adapter: adapter, Hasher: hasher, Store: st, Log: log}
	res, err := runner.Run(ctx, source.Window{From: day, To: day})
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if res.Records == 0 {
		t.Fatal("the poller stored no records")
	}

	count := func() int {
		return queryInt(t, endpoint, user, pass, "SELECT count() FROM cardo.bronze_actor_day WHERE org_id = '"+orgID+"'")
	}
	if got := count(); got == 0 {
		t.Fatal("no rows in bronze_actor_day for the test organization")
	}
	pseudonymValue := queryString(t, endpoint, user, pass, "SELECT any(pseudonym) FROM cardo.bronze_actor_day WHERE org_id = '"+orgID+"'")
	if strings.Contains(pseudonymValue, "@") || pseudonymValue == "" {
		t.Fatalf("pseudonym %q is empty or carries the email", pseudonymValue)
	}
	if got := queryInt(t, endpoint, user, pass, "SELECT count() FROM cardo.silver_actor_day WHERE pseudonym = '"+pseudonymValue+"'"); got == 0 {
		t.Fatal("silver_actor_day has no row for the polled actor")
	}

	// A second pass over the same day replaces the day instead of doubling it.
	before := count()
	if _, err := runner.Run(ctx, source.Window{From: day, To: day}); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if after := count(); after != before {
		t.Fatalf("re-poll changed the row count: %d then %d (a day must be replaced, not appended)", before, after)
	}

	// The board's organization card reads the same store: configured, and it sees the day.
	dir := t.TempDir()
	env := map[string]string{
		"CARDO_ADMIN_KEY":     "sk-ant-e2e",
		"CARDO_POLL_INTERVAL": "24h",
		"CLICKHOUSE_USER":     user,
		"CLICKHOUSE_PASSWORD": pass,
		"CARDO_SALT":          strings.Repeat("ab", 32),
		"CARDO_SALT_VERSION":  "v1",
		"GRAFANA_USER":        "admin",
		"GRAFANA_PASSWORD":    "unused",
	}
	if err := launcher.WriteDotEnv(dir+"/.env", env, launcher.Secrets{}); err != nil {
		t.Fatal(err)
	}
	srvBoard, err := launcher.NewServer(launcher.Options{Dir: dir, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	req.Header.Set("X-Cardo-Token", srvBoard.Token())
	rec := httptest.NewRecorder()
	srvBoard.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin = %d: %s", rec.Code, rec.Body.String())
	}
	var view struct {
		Configured bool   `json:"configured"`
		LastDay    string `json:"lastDay"`
		Rows       int    `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Configured || view.Rows == 0 || view.LastDay == "" {
		t.Fatalf("board status = %+v, want configured with rows and a day", view)
	}
	t.Logf("polled %d records across %d days; bronze rows=%d, last day=%s", res.Records, res.DaysWithData, view.Rows, view.LastDay)
}

// retag points the fixture at the test organization and yesterday, so the run is repeatable and the
// fleet views never see a fake organization.
func retag(t *testing.T, raw []byte, org string, day time.Time) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	rows, _ := payload["data"].([]any)
	for _, r := range rows {
		m, _ := r.(map[string]any)
		m["organization_id"] = org
		m["date"] = day.Format(time.RFC3339)
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func clickhouseCredentials(t *testing.T) (string, string) {
	t.Helper()
	env, err := launcher.ReadDotEnv("../deploy/compose/.env")
	if err != nil {
		t.Skipf("no deploy/compose/.env: %v", err)
	}
	user, pass := env["CLICKHOUSE_USER"], env["CLICKHOUSE_PASSWORD"]
	if user == "" || pass == "" {
		t.Skip("deploy/compose/.env has no ClickHouse credentials")
	}
	return user, pass
}

func cleanupOrg(t *testing.T, endpoint, user, pass, org string) {
	t.Helper()
	for _, table := range []string{"cardo.bronze_actor_day", "cardo.bronze_actor_day_staging"} {
		sql := fmt.Sprintf("ALTER TABLE %s DELETE WHERE org_id = '%s' SETTINGS mutations_sync = 1", table, org)
		if err := chExec(endpoint, user, pass, sql); err != nil {
			t.Errorf("cleanup %s: %v", table, err)
		}
	}
}

func chExec(endpoint, user, pass, sql string) error {
	req, err := http.NewRequest(http.MethodPost, endpoint+"/?user="+user+"&password="+pass, strings.NewReader(sql))
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(b.String()))
	}
	return nil
}

func queryInt(t *testing.T, endpoint, user, pass, sql string) int {
	t.Helper()
	out := queryString(t, endpoint, user, pass, sql)
	var n int
	if _, err := fmt.Sscan(strings.TrimSpace(out), &n); err != nil {
		t.Fatalf("query %q returned %q", sql, out)
	}
	return n
}

func queryString(t *testing.T, endpoint, user, pass, sql string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint+"/?user="+user+"&password="+pass, strings.NewReader(sql))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query %q: %s: %s", sql, resp.Status, b.String())
	}
	return strings.TrimSpace(b.String())
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
