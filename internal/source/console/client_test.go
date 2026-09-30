package console

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "console", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// serve returns a Client pointed at a test server that replays the given fixtures in order, and a
// pointer to the recorded requests so tests can assert on what was actually sent.
func serve(t *testing.T, bodies ...[]byte) (*Client, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Clone(r.Context()))
		if i >= len(bodies) {
			t.Errorf("unexpected extra request: %s", r.URL)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(bodies[i])
		i++
	}))
	t.Cleanup(srv.Close)

	c, err := New("sk-ant-admin-test", quietLogger(), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c, &got
}

func day() time.Time { return time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC) }

func TestFetchDecodesDocumentedResponse(t *testing.T) {
	c, reqs := serve(t, fixture(t, "single-page.json"))

	recs, err := c.Fetch(context.Background(), day())
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}

	r := recs[0]
	if r.ActorID != "Developer@Example.com" {
		t.Errorf("ActorID = %q", r.ActorID)
	}
	if strings.Join(r.ActorPath, ".") != "actor.email_address" {
		t.Errorf("ActorPath = %v, want actor.email_address", r.ActorPath)
	}
	if r.OrgID != "dc9f6c26-b22c-4831-8d01-0446bada88f1" || r.CustomerType != "api" || r.TerminalType != "vscode" {
		t.Errorf("dimensions wrong: %+v", r)
	}
	if len(r.Unknown) != 0 {
		t.Errorf("documented fixture reported drift: %v", r.Unknown)
	}

	// The payload must reach storage exactly as the API sent it (ADR-0010).
	var raw map[string]any
	if err := json.Unmarshal(r.Raw, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["core_metrics"]; !ok {
		t.Error("raw payload lost core_metrics")
	}

	req := (*reqs)[0]
	if got := req.URL.Query().Get("starting_at"); got != "2026-09-08" {
		t.Errorf("starting_at = %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "sk-ant-admin-test" {
		t.Errorf("x-api-key header = %q", got)
	}
	if got := req.Header.Get("anthropic-version"); got != apiVersion {
		t.Errorf("anthropic-version = %q, want %q", got, apiVersion)
	}
	if !strings.HasPrefix(req.Header.Get("User-Agent"), "Cardo/") {
		t.Errorf("User-Agent = %q, want a Cardo integration identifier", req.Header.Get("User-Agent"))
	}
}

// Cost is documented in cents. Reading it as dollars makes every figure on every dashboard 100x
// too large, which is the kind of error that survives a demo and embarrasses later.
func TestCostIsParsedAsCents(t *testing.T) {
	var p page
	if err := json.Unmarshal(fixture(t, "single-page.json"), &p); err != nil {
		t.Fatal(err)
	}
	got := p.Data[0].ModelUsage[0].Cost.AmountCents
	if got != 113 {
		t.Fatalf("AmountCents = %d, want 113 (that is $1.13, not $113)", got)
	}
}

func TestFetchFollowsPagination(t *testing.T) {
	c, reqs := serve(t, fixture(t, "page-1.json"), fixture(t, "page-2.json"))

	recs, err := c.Fetch(context.Background(), day())
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records across two pages, want 2", len(recs))
	}
	if len(*reqs) != 2 {
		t.Fatalf("made %d requests, want 2", len(*reqs))
	}
	if got := (*reqs)[0].URL.Query().Get("page"); got != "" {
		t.Errorf("first request sent a cursor: %q", got)
	}
	if got := (*reqs)[1].URL.Query().Get("page"); got != "page_MjAyNS0wNS0xNFQwMDowMDowMFo=" {
		t.Errorf("second request cursor = %q, want the one from page 1", got)
	}
}

// ADR-0014: an unmodelled field is reported, never dropped, and never fatal.
func TestFetchReportsDriftWithoutFailing(t *testing.T) {
	c, _ := serve(t, fixture(t, "drift.json"))

	recs, err := c.Fetch(context.Background(), day())
	if err != nil {
		t.Fatalf("drift must not fail the poll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}

	want := map[string]bool{
		"workspace_id":                     true,
		"core_metrics.skills_invoked":      true,
		"tool_actions.bash_tool":           true,
		"model_breakdown.tokens.reasoning": true,
	}
	got := map[string]bool{}
	for _, p := range recs[0].Unknown {
		got[p] = true
	}
	for path := range want {
		if !got[path] {
			t.Errorf("drift at %q was not reported; reported: %v", path, recs[0].Unknown)
		}
	}
	if !strings.Contains(string(recs[0].Raw), "skills_invoked") {
		t.Error("an unmodelled field was dropped from the raw payload")
	}
}

func TestFetchHandlesAPIKeyActor(t *testing.T) {
	c, _ := serve(t, fixture(t, "api-actor.json"))

	recs, err := c.Fetch(context.Background(), day())
	if err != nil {
		t.Fatal(err)
	}
	if recs[0].ActorID != "ci-pipeline-key" {
		t.Errorf("ActorID = %q, want ci-pipeline-key", recs[0].ActorID)
	}
	if strings.Join(recs[0].ActorPath, ".") != "actor.api_key_name" {
		t.Errorf("ActorPath = %v", recs[0].ActorPath)
	}
}

func TestFetchHandlesEmptyDay(t *testing.T) {
	c, _ := serve(t, fixture(t, "empty.json"))

	recs, err := c.Fetch(context.Background(), day())
	if err != nil {
		t.Fatalf("an empty day is a successful response, not an error: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("got %d records, want 0", len(recs))
	}
}
