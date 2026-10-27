// Command cardo-madeup invents a made-up organization's Claude Code sessions and sends them to the
// made-up stack's collector, as many copies of Claude Code would (ADR-0051).
//
// It is how Cardo's views are seen, and checked, on an organization's worth of sessions before any
// organization's real data exists. Nothing seen in what it sends is a finding: it shows that a
// view shows what was put in, never anything about real people.
//
// It sends only to the made-up stack (deploy/compose/docker-compose.made-up.yml), whose addresses
// are fixed in internal/madeup. Run it through scripts/made-up.sh load.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/madeup"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "\ncardo-madeup: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("cardo-madeup", flag.ContinueOnError)
	preset := fs.String("preset", "review", "the organization's size: "+strings.Join(presetNames(), ", "))
	seed := fs.Uint64("seed", 1, "the seed: the same seed, preset and start always give the same organization")
	start := fs.String("start", "", "the first day, a Monday, as 2006-01-02 (default: the Monday as many weeks before this week's as the history covers)")
	weeks := fs.Int("weeks", 0, "weeks of history (default: the preset's)")
	keyPath := fs.String("key", "", "write the answer key to this file")
	dryRun := fs.Bool("dry-run", false, "invent the organization and write the answer key, but send nothing")
	workers := fs.Int("workers", 16, "requests in flight to the collector at once")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, ok := madeup.Presets[*preset]
	if !ok {
		return fmt.Errorf("no preset %q; there are: %s", *preset, strings.Join(presetNames(), ", "))
	}
	if *weeks == 0 {
		*weeks = p.Weeks
	}
	first, err := firstDay(*start, *weeks)
	if err != nil {
		return err
	}
	o, err := madeup.NewOrg(p, *seed, first, *weeks)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "Made-up organization: %d people in %d teams, %s to %s, seed %d.\n",
		len(o.People), len(p.Teams), o.Start.Format(time.DateOnly),
		o.End().AddDate(0, 0, -1).Format(time.DateOnly), *seed)

	began := time.Now()
	var key *madeup.Key
	if *dryRun {
		key, err = madeup.Generate(ctx, o, discard{}, *workers)
		if err != nil {
			return err
		}
	} else {
		sink, err := madeup.NewHTTPSink(ctx, *workers)
		if err != nil {
			return err
		}
		done := make(chan struct{})
		go progress(sink, began, done)
		key, err = madeup.Generate(ctx, o, sink, *workers)
		if closeErr := sink.Close(); err == nil {
			err = closeErr
		}
		close(done)
		if err != nil {
			return err
		}
	}

	t := key.Totals
	verb := "Sent"
	if *dryRun {
		verb = "Invented, and sent nothing:"
	}
	fmt.Fprintf(os.Stderr, "%s %d sessions by %d people: %d hook events, %d log records, %d metric points, "+
		"$%.0f of made-up spend, in %s.\n", verb, t.Sessions, t.People, t.HookEvents, t.LogRecords,
		t.MetricPoints, t.CostUSD, time.Since(began).Round(time.Second))
	if *keyPath != "" {
		if err := key.WriteFile(*keyPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Answer key: %s\n", *keyPath)
	}
	return nil
}

// firstDay is the given Monday, or by default the Monday that many weeks before this week's, so
// the history ends on the Sunday just gone and all of it is inside the 90 days ClickHouse keeps.
func firstDay(given string, weeks int) (time.Time, error) {
	if given != "" {
		d, err := time.Parse(time.DateOnly, given)
		if err != nil {
			return time.Time{}, fmt.Errorf("-start %q: %w", given, err)
		}
		if d.Weekday() != time.Monday {
			return time.Time{}, errors.New("-start must be a Monday, so the history's weeks are the views' weeks")
		}
		return d, nil
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	thisMonday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	return thisMonday.AddDate(0, 0, -7*weeks), nil
}

func progress(s *madeup.HTTPSink, began time.Time, done <-chan struct{}) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			fmt.Fprintf(os.Stderr, "  %s: %d hook events, %d log requests, %d metric requests sent; %d retried\n",
				time.Since(began).Round(time.Second), s.Hooks.Load(), s.LogRequests.Load(),
				s.MetricRequests.Load(), s.Retries.Load())
		}
	}
}

func presetNames() []string {
	var out []string
	for name := range madeup.Presets {
		out = append(out, name)
	}
	return out
}

type discard struct{}

func (discard) Hook(time.Time, []byte) error { return nil }
func (discard) Logs([]byte) error            { return nil }
func (discard) Metrics([]byte) error         { return nil }
