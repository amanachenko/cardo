// Package poll runs one backfill or incremental poll: fetch, scrub, store.
//
// The pipeline is deliberately linear and has exactly one branch point of consequence — whether
// the whole window came back empty. See Result.Diagnose and risks.md #9.
package poll

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/store"
)

// Runner wires an adapter to a store through the pseudonymizer.
type Runner struct {
	Adapter source.Adapter
	Hasher  *pseudonym.Hasher
	Store   store.Store
	Log     *slog.Logger
}

// Result summarises a poll.
type Result struct {
	Source       source.Name
	Window       source.Window
	DaysPolled   int
	DaysWithData int
	Records      int
	Actors       map[string]struct{}
	Unknown      map[string]int
}

// Run polls every day in the window, oldest first.
//
// Days are processed oldest first so that an interrupted backfill leaves a contiguous prefix of
// history rather than holes, and so a resumed run can start from the last complete day.
func (r *Runner) Run(ctx context.Context, w source.Window) (*Result, error) {
	res := &Result{
		Source:  r.Adapter.Name(),
		Window:  w,
		Actors:  map[string]struct{}{},
		Unknown: map[string]int{},
	}

	for _, day := range w.Days() {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		key := day.Format("2006-01-02")

		records, err := r.Adapter.Fetch(ctx, day)
		if err != nil {
			return res, fmt.Errorf("fetching %s: %w", key, err)
		}
		res.DaysPolled++

		scrubbed := make([]pseudonym.Scrubbed, 0, len(records))
		for _, rec := range records {
			s, err := r.Hasher.Scrub(rec)
			if err != nil {
				// A scrubbing failure is never skipped. An identity leak means the source
				// carries identity somewhere unmodelled, and continuing past it would write a
				// real email address into bronze — the one thing INV-2 forbids absolutely.
				var leak pseudonym.ErrIdentityLeak
				if errors.As(err, &leak) {
					return res, fmt.Errorf(
						"%w\n  Stopping the poll. Nothing for %s has been written. "+
							"Compare the response against docs/research/ and add the new "+
							"identity field to the adapter before running again", err, key)
				}
				return res, fmt.Errorf("scrubbing %s: %w", key, err)
			}
			scrubbed = append(scrubbed, s)
			res.Actors[s.Pseudonym()] = struct{}{}
			for _, p := range s.Unknown() {
				res.Unknown[p]++
			}
		}

		if err := r.Store.Put(ctx, r.Adapter.Name(), key, scrubbed); err != nil {
			return res, fmt.Errorf("storing %s: %w", key, err)
		}

		res.Records += len(scrubbed)
		if len(scrubbed) > 0 {
			res.DaysWithData++
		}
		r.Log.Info("day stored", "day", key, "records", len(scrubbed))
	}

	return res, nil
}

// Diagnose reports the operator-facing conclusion of a poll.
//
// The case this exists for is risks.md #9: an Admin API key belonging to a Claude Enterprise
// organization authenticates successfully against the Console endpoint and returns zero rows,
// because Enterprise Claude Code activity is reported by a different API entirely. No error is
// raised anywhere. A correct install at a quiet organization looks identical.
//
// That is the worst failure shape available — it wastes the first impression, and the operator's
// natural conclusion is that the tool does not work. So an empty window is reported loudly, with
// the distinction named, rather than logged as a successful run of zero records.
func (r *Result) Diagnose() (level slog.Level, msg string, attrs []any) {
	attrs = []any{
		"source", r.Source,
		"days_polled", r.DaysPolled,
		"days_with_data", r.DaysWithData,
		"records", r.Records,
		"distinct_actors", len(r.Actors),
	}

	if r.Records == 0 && r.DaysPolled > 0 {
		return slog.LevelWarn, fmt.Sprintf(
			"polled %d days and found no Claude Code activity at all.\n"+
				"  This is a successful, authenticated, empty result — which has two very\n"+
				"  different causes, and they are worth telling apart before anything else:\n"+
				"\n"+
				"    1. The organization genuinely had no Claude Code usage in this window.\n"+
				"    2. The organization is a Claude Enterprise (claude.ai) org. Its Claude Code\n"+
				"       activity is reported by the Claude Enterprise Analytics API, which is a\n"+
				"       different endpoint requiring a different key type, and this endpoint will\n"+
				"       keep returning zero rows no matter how long you poll it.\n"+
				"\n"+
				"  Check which product the organization is on before assuming (1). See ADR-0021\n"+
				"  and docs/research/2026-09-23-admin-analytics-apis.md.",
			r.DaysPolled), attrs
	}

	if len(r.Unknown) > 0 {
		paths := make([]string, 0, len(r.Unknown))
		for p := range r.Unknown {
			paths = append(paths, p)
		}
		attrs = append(attrs, "unmodelled_fields", paths)
		return slog.LevelWarn,
			"poll complete, but the response carried fields this adapter does not model. " +
				"They are stored verbatim in bronze and are not lost; the canonical views need updating",
			attrs
	}

	return slog.LevelInfo, "poll complete", attrs
}

// DefaultWindow returns the range to poll when none is given.
//
// It ends yesterday rather than today. The Console endpoint excludes data under an hour old, so
// today is always partial; storing it would mean the most recent day on every dashboard is a
// number that silently grows all day and is never right.
func DefaultWindow(now time.Time, days int) source.Window {
	end := now.UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	return source.Window{From: end.AddDate(0, 0, -(days - 1)), To: end}
}
