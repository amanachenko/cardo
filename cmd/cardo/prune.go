package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/amanachenko/cardo/internal/retention"
)

// runPrune deletes raw rows older than the window. It is the manual twin of the poller's automatic
// pass; both use the same table list.
func runPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	useSynopsis(fs, "prune")
	days := fs.Int("days", retentionFromEnv(), "delete raw rows older than this many days")
	dry := fs.Bool("dry-run", false, "count what would be deleted, delete nothing")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	if *days <= 0 {
		return fmt.Errorf("retention is off: set -days N or CARDO_RETENTION_DAYS, or change it in Settings")
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

func retentionFromEnv() int {
	n, _ := strconv.Atoi(os.Getenv("CARDO_RETENTION_DAYS"))
	return n
}
