package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serveStatus(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := New("sk-ant-admin-test", quietLogger(), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A 403 is the shape an Enterprise organization's key produces against this endpoint. A bare
// "403 Forbidden" sends the operator looking at roles and permissions, which is the wrong place.
// Naming the Console/Enterprise split here is the difference between a two-minute fix and an
// afternoon — see ADR-0021 and risks.md #9.
func TestForbiddenNamesTheEnterpriseDistinction(t *testing.T) {
	c := serveStatus(t, http.StatusForbidden, `{"error":"forbidden"}`)

	_, err := c.Fetch(context.Background(), day())
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"Enterprise", "Analytics API", "ADR-0021", "admin role"} {
		if !strings.Contains(msg, want) {
			t.Errorf("403 message should mention %q; got:\n%s", want, msg)
		}
	}
}

func TestUnauthorizedExplainsTheKeyType(t *testing.T) {
	c := serveStatus(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)

	_, err := c.Fetch(context.Background(), day())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"sk-ant-admin", "Admin keys", "workspace-scoped"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("401 message should mention %q; got:\n%s", want, err)
		}
	}
}

// A client error must fail immediately. Retrying a 403 five times wastes the operator's time and
// tells them nothing new.
func TestClientErrorsAreNotRetried(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c, _ := New("k", quietLogger(), WithBaseURL(srv.URL))

	if _, err := c.Fetch(context.Background(), day()); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("made %d requests for a 403, want 1", calls)
	}
}

// A 500 or a 429 is transient and must be retried, or a long backfill dies on one blip.
func TestServerErrorsAreRetriedThenSucceed(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"data":[],"has_more":false,"next_page":null}`))
	}))
	defer srv.Close()
	c, _ := New("k", quietLogger(), WithBaseURL(srv.URL),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.Fetch(ctx, day()); err != nil {
		t.Fatalf("transient failures should be retried: %v", err)
	}
	if calls != 3 {
		t.Fatalf("made %d requests, want 3 (two failures then success)", calls)
	}
}

func TestRateLimitHintPointsAtTheUncertainty(t *testing.T) {
	err := classify(http.StatusTooManyRequests, []byte(`{"error":"rate_limited"}`), day())
	if !strings.Contains(err.Error(), "undocumented") {
		t.Errorf("429 hint should flag that backfill pacing is unvalidated; got:\n%s", err)
	}
}

// Pagination must terminate. A cursor loop that never ends would burn an admin key's rate limit
// silently and look like a hang.
func TestPaginationIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"has_more":true,"next_page":"always-more"}`))
	}))
	defer srv.Close()
	c, _ := New("k", quietLogger(), WithBaseURL(srv.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := c.Fetch(ctx, day())
	if err == nil {
		t.Fatal("expected pagination to be bounded")
	}
	if !strings.Contains(err.Error(), "did not terminate") {
		t.Errorf("error should name the runaway cursor; got: %v", err)
	}
}
