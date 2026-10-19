package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func adminEnv(t *testing.T, body string) (Options, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	dir := t.TempDir()
	if err := WriteDotEnv(dir+"/.env", map[string]string{
		"CLICKHOUSE_USER": "cardo", "CLICKHOUSE_PASSWORD": "secret",
	}, Secrets{}); err != nil {
		t.Fatal(err)
	}
	return Options{Dir: dir, URLFn: func(string, string) string { return srv.URL }}, srv.Close
}

func TestAdminDataIsFresh(t *testing.T) {
	day := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	opts, done := adminEnv(t, day+"\t6\t3\n")
	defer done()
	r, err := checkAdminData(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || !strings.Contains(r.Value, "3 people") {
		t.Fatalf("fresh admin rows must pass, got %+v", r)
	}
}

func TestAdminDataFlagsAStalePoller(t *testing.T) {
	opts, done := adminEnv(t, "2020-01-01\t6\t3\n")
	defer done()
	r, err := checkAdminData(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK || !strings.Contains(r.Cause, "2020-01-01") {
		t.Fatalf("stale rows must fail with the date, got %+v", r)
	}
}

func TestAdminDataWaitsForTheFirstPoll(t *testing.T) {
	opts, done := adminEnv(t, "1970-01-01\t0\t0\n")
	defer done()
	r, err := checkAdminData(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Pending || r.OK {
		t.Fatalf("no rows must be pending, got %+v", r)
	}
}
