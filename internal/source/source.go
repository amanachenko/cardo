// Package source defines the contract every analytics adapter implements.
//
// ADR-0021 commits v1 to two sources — Claude Console organizations via the Claude Code Analytics
// API, and Claude Enterprise organizations via the Claude Enterprise Analytics API. They differ in
// credential type, endpoint, freshness, history floor and cost encoding. Those differences are
// adapter concerns and are absorbed here, so that nothing downstream of this package needs to know
// which source a row came from beyond the Source discriminator.
package source

import (
	"context"
	"encoding/json"
	"time"
)

// Name identifies which adapter produced a record. Stored on every bronze row (ADR-0021) so that
// adding the second adapter is additive rather than a schema migration.
type Name string

const (
	// Console is the Claude Code Analytics API, reached with an Admin API key.
	Console Name = "console"
	// Enterprise is the Claude Enterprise Analytics API, reached with an Analytics API key.
	// Declared here because the discriminator must exist from the first row ever written; the
	// adapter itself is not built. See risks.md #8.
	Enterprise Name = "enterprise"
)

// Record is one actor's activity for one day, as returned by a source, before pseudonymization.
//
// A Record still carries a real identity in ActorID and inside Raw. It must never reach a store.
// The type system enforces that: stores accept only pseudonym.Scrubbed, which can only be produced
// by passing a Record through a Hasher.
type Record struct {
	// Source names the adapter that produced this record.
	Source Name

	// Day is the UTC calendar day the metrics cover.
	Day time.Time

	// ActorType distinguishes a human actor from an API key actor.
	ActorType string

	// ActorID is the raw identifier — an email address or an API key name. This field is the
	// reason Record cannot be persisted.
	ActorID string

	// ActorPath is the JSON path to ActorID inside Raw, so the scrubber can remove it without
	// knowing any source's payload shape. For example: ["actor", "email_address"].
	ActorPath []string

	// OrgID, CustomerType and TerminalType are non-identifying dimensions.
	OrgID        string
	CustomerType string
	TerminalType string

	// Raw is the source's response object for this record, verbatim. ADR-0010 stores payloads raw
	// so that a field we parsed wrongly is a view rewrite rather than a re-ingest.
	//
	// "Verbatim" has exactly one exception, and it is not optional: the actor identifier is
	// replaced during scrubbing. INV-2 outranks ADR-0010 where they meet.
	Raw json.RawMessage

	// Unknown lists JSON field paths present in the response that this adapter does not model.
	// Reported loudly rather than dropped silently, per ADR-0014 — the first live call against a
	// real key is expected to surface drift here.
	Unknown []string
}

// Window describes the range of days a poll should cover.
type Window struct {
	From time.Time
	To   time.Time
}

// Days returns each UTC day in the window, oldest first. Both endpoints are inclusive, because
// both analytics APIs address a single day per request rather than a range.
func (w Window) Days() []time.Time {
	var out []time.Time
	for d := w.From.UTC().Truncate(24 * time.Hour); !d.After(w.To.UTC().Truncate(24 * time.Hour)); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

// Adapter fetches records for a single day.
//
// Implementations own their own pagination, rate limiting and freshness rules. Fetch returns
// ErrDayNotAvailable when a source declines a day that is too recent; the caller treats that as a
// stopping condition, not a failure.
type Adapter interface {
	// Name reports which source this adapter speaks for.
	Name() Name

	// Fetch returns every record for one UTC day, following pagination to completion.
	Fetch(ctx context.Context, day time.Time) ([]Record, error)
}
