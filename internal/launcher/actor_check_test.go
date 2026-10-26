package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func actorEnv(t *testing.T, body string) (Options, func()) {
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

func TestActorTypesBothDocumentedTypesPass(t *testing.T) {
	opts, done := actorEnv(t, "user_actor\t3\napi_actor\t2\n")
	defer done()
	r, err := checkActorTypes(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || !strings.Contains(r.Value, "user_actor 3") || !strings.Contains(r.Value, "api_actor 2") {
		t.Fatalf("want both documented types counted, got %+v", r)
	}
}

func TestActorTypesFlagsAnUnknownType(t *testing.T) {
	opts, done := actorEnv(t, "user_actor\t3\nservice_account_actor\t1\n")
	defer done()
	r, err := checkActorTypes(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK {
		t.Fatal("an undocumented actor type must not pass")
	}
	if !strings.Contains(r.Cause, "service_account_actor") {
		t.Fatalf("cause must name the unknown type, got %q", r.Cause)
	}
}

func TestActorTypesWaitsForTheFirstPoll(t *testing.T) {
	opts, done := actorEnv(t, "")
	defer done()
	r, err := checkActorTypes(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Pending || r.OK {
		t.Fatalf("no rows must be pending, got %+v", r)
	}
}
