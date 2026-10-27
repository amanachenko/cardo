package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/retention"
)

// runPrune deletes raw rows older than the window. It is the manual twin of the poller's automatic
// pass; both use the same table list and the same window.
func runPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	useSynopsis(fs, "prune")
	dir := fs.String("dir", filepath.Join("deploy", "compose"), "stack directory that holds .env")
	days := fs.Int("days", -1, "delete raw rows older than this many days; 0 keeps everything")
	dry := fs.Bool("dry-run", false, "count what would be deleted, delete nothing")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	endpoint, user, pass := clickhouseConn(*dir)
	// Without an explicit -days, the window in force is the one the operator chose in the console.
	// The environment is the fallback for an installation whose console has never been opened.
	// A prune that used the environment alone would put the TTL back to a window the operator has
	// already changed.
	if *days < 0 {
		effective, err := retention.EffectiveWindow(ctx, endpoint, user, pass, retentionFromEnv())
		if err != nil {
			return err
		}
		*days = effective
	}
	if *days < 0 {
		return fmt.Errorf("retention window must be zero or more days")
	}
	if *days == 0 {
		fmt.Println("Keeping everything: no row is deleted, and the tables' TTL is taken off.")
	}
	report, err := retention.Prune(ctx, endpoint, user, pass, *days, *dry)
	if err != nil {
		return err
	}
	verb := "deleted"
	if *dry {
		verb = "would delete"
	}
	for table, n := range report {
		fmt.Printf("  %-32s %6d rows %s\n", table, n, verb)
	}
	return nil
}

// retentionFromEnv returns the window the operator chose in the stack's .env. An unset variable
// means the default window (ADR-0053), so a fresh installation already deletes on the published
// schedule rather than keeping everything. Zero keeps everything, and says so.
func retentionFromEnv() int {
	raw := strings.TrimSpace(stackEnv(stackDir(""))["CARDO_RETENTION_DAYS"])
	if raw == "" {
		return retention.DefaultDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return retention.DefaultDays
	}
	return n
}
