// Package store defines where scrubbed records are persisted.
//
// ADR-0009 makes the semantic layer storage-agnostic: ClickHouse is the reference stack and a
// no-infrastructure mode exists so a small pilot needs nothing approved. Both must work.
//
// Every implementation accepts pseudonym.Scrubbed and nothing else. That is not a style choice —
// it is how INV-2 is enforced. A record carrying a real identity cannot be passed to a store
// because it will not compile.
package store

import (
	"context"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
)

// Store persists scrubbed records.
type Store interface {
	// Migrate brings the store's schema up to date. Safe to call on every run.
	Migrate(ctx context.Context) error

	// Put writes all records for one source and one day, replacing anything already held for
	// that key.
	//
	// Replace rather than append, because both analytics APIs revise recent days — the Console
	// endpoint excludes data under an hour old, and Enterprise cost figures are revised for up
	// to thirty days. A poller that appended would accumulate duplicate, diverging copies of the
	// same day and quietly double every total.
	Put(ctx context.Context, src source.Name, day string, records []pseudonym.Scrubbed) error

	// Close releases resources.
	Close() error
}
