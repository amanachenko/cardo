package console

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// APIError is a non-200 response from the analytics endpoint.
type APIError struct {
	StatusCode int
	Day        time.Time
	Body       string
	// Hint carries operator-facing guidance where the status code alone is misleading.
	Hint string
	// RetryAfter is the delay the server asked for, when it asked for one.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("Claude Code Analytics API returned %d for %s", e.StatusCode, e.Day.Format("2006-01-02"))
	if e.Body != "" {
		msg += ": " + e.Body
	}
	if e.Hint != "" {
		msg += "\n  " + e.Hint
	}
	return msg
}

// Retryable reports whether trying the same request again could succeed.
func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// classify turns an HTTP failure into an error carrying operator-facing guidance.
//
// The authentication cases matter more than usual here. An Admin API key and an Analytics API key
// are not interchangeable (ADR-0021), and an operator holding the wrong one gets a bare 401 or 403
// that says nothing about why. Naming the distinction in the error is the difference between a
// two-minute fix and an afternoon.
func classify(status int, body []byte, day time.Time) error {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 500 {
		trimmed = trimmed[:500] + "…"
	}

	e := &APIError{StatusCode: status, Day: day, Body: trimmed}
	switch status {
	case http.StatusUnauthorized:
		e.Hint = "The key was rejected. This endpoint needs an Admin API key (sk-ant-admin...) " +
			"provisioned by an organization admin in Console > Settings > Admin keys. " +
			"A workspace-scoped key will not work."
	case http.StatusForbidden:
		e.Hint = "Authenticated but not authorised. The key reached Anthropic and was accepted; " +
			"the account behind it is not allowed to read this endpoint. Three causes, most " +
			"common first:\n" +
			"  1. The account is an individual account rather than an organization. The Admin " +
			"API is unavailable to individual accounts, and that is an account-type rule, not a " +
			"key-type one: a correctly org-scoped personal key still gets this 403. Verified " +
			"against a live individual account -- every Admin endpoint returned 403 except " +
			"GET /v1/organizations/me, which returns 200 and the org's name. Do not use /me to " +
			"check whether a key works; it answers for accounts that can read nothing else.\n" +
			"  2. The key belongs to an organization member without the admin role.\n" +
			"  3. The organization is a Claude Enterprise (claude.ai) org, which reports Claude " +
			"Code activity through the Claude Enterprise Analytics API instead -- a different " +
			"endpoint and a different key type. See ADR-0021."
	case http.StatusNotFound:
		e.Hint = "Endpoint not found. If this organization is on Claude Platform on AWS, the " +
			"Claude Code Analytics API is not available; use the Console Usage page."
	case http.StatusTooManyRequests:
		e.Hint = "Rate limited. Backfill pacing for this endpoint is undocumented — see " +
			"docs/research/2026-09-23-admin-analytics-apis.md, uncertainty 4."
	}
	return e
}

// retryAfter returns the delay a response asks for, or zero.
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := time.ParseDuration(v + "s"); err == nil && secs > 0 {
		return secs
	}
	return 0
}

// backoff returns the delay before attempt n, capped so a long backfill cannot stall indefinitely.
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<attempt) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// isRetryable reports whether an error is worth another attempt.
func isRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	// Transport errors — connection resets, timeouts — are worth retrying. A context
	// cancellation is not, and is filtered by the caller before reaching here.
	return err != nil
}
