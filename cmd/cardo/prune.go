package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/retention"
)

// runPrune deletes raw rows older than the window. It is the manual twin of the poller's automatic
// pass; both use the same table list.
func runPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	useSynopsis(fs, "prune")
	days := fs.Int("days", retentionFromEnv(), "delete raw rows older than this many days; 0 keeps everything")
	dry := fs.Bool("dry-run", false, "count what would be deleted, delete nothing")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	if *days < 0 {
		return fmt.Errorf("retention window must be zero or more days")
	}
	if *days == 0 {
		fmt.Println("Keeping everything: no row is deleted, and the tables' TTL is taken off.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := retention.Prune(ctx, os.Getenv("CARDO_CLICKHOUSE_URL"), envOr("CARDO_CLICKHOUSE_USER", "default"), os.Getenv("CARDO_CLICKHOUSE_PASSWORD"), *days, *dry)
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

// retentionFromEnv returns the window the operator chose. An unset variable means the default
// window (ADR-0053), so a fresh installation already deletes on the published schedule rather
// than keeping everything. Zero keeps everything, and says so.
func retentionFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("CARDO_RETENTION_DAYS"))
	if raw == "" {
		return retention.DefaultDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return retention.DefaultDays
	}
	return n
}
