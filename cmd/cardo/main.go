// Command cardo polls Claude Code analytics into a local store.
//
// This is the Phase 1 wedge from ADR-0017: it needs nothing deployed to any developer machine —
// only an Admin API key — and it exists to prove the analysis layer before anyone is asked to
// approve a managed-settings rollout.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amanachenko/cardo/internal/poll"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/source/console"
	"github.com/amanachenko/cardo/internal/store"
	"github.com/amanachenko/cardo/internal/store/clickhouse"
	"github.com/amanachenko/cardo/internal/store/jsonl"
)

var version = "0.1.0-dev"

const usage = `cardo — self-hosted observability for Claude Code

Usage:
  cardo poll [flags]      Poll an analytics source into a local store
  cardo migrate [-v]      Apply the ClickHouse schema and exit (needs no salt or key)
  cardo version           Print the version

Secrets are read from the environment, never from flags, because command-line
arguments are visible to every other process on the machine:

  CARDO_ADMIN_KEY         Admin API key (sk-ant-admin...), provisioned by an
                          organization admin in Console > Settings > Admin keys
  CARDO_SALT              Pseudonymization salt, at least 32 hex characters
                          (openssl rand -hex 32). Held by the organization's
                          security team; never stored beside the data it
                          protects and never committed (ADR-0006)
  CARDO_SALT_VERSION      Label for the current salt, stored beside every
                          pseudonym so rotation stays possible. Default: v1

With -store clickhouse the destination is read from the environment too. There
is no default endpoint: it is your infrastructure and cardo will not guess.

  CARDO_CLICKHOUSE_URL    HTTP interface of your ClickHouse server, including
                          scheme and port (a stock server listens on 8123)
  CARDO_CLICKHOUSE_USER   Defaults to "default"
  CARDO_CLICKHOUSE_PASSWORD

migrate also stores two choices the collector's views read. Unset leaves the
stored value alone; set to "" resets it to the default:

  CARDO_ORG_ARTIFACTS     The regex naming your organization's own artifacts. Give
                          it the collector's value
  CARDO_MIN_GROUP_SIZE    People a home-grown artifact or a cohort needs before a
                          view names it. At least 5, the default (ADR-0029)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "\ncardo: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return errors.New("no command given")
	}

	switch args[0] {
	case "poll":
		return runPoll(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runPoll(args []string) error {
	fs := flag.NewFlagSet("poll", flag.ContinueOnError)
	var (
		srcName   = fs.String("source", "console", "analytics source: console")
		storeKind = fs.String("store", "file", "storage target: file | clickhouse")
		out       = fs.String("out", "./data", "directory for the local store (-store file)")
		days      = fs.Int("days", 30, "number of days to poll, ending at -to")
		from      = fs.String("from", "", "first day to poll, YYYY-MM-DD (overrides -days)")
		to        = fs.String("to", "", "last day to poll, YYYY-MM-DD (defaults to yesterday)")
		verbose   = fs.Bool("v", false, "verbose logging")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	hasher, err := newHasher()
	if err != nil {
		return err
	}

	adapter, err := newAdapter(*srcName, log)
	if err != nil {
		return err
	}

	window, err := resolveWindow(time.Now(), *from, *to, *days)
	if err != nil {
		return err
	}

	st, err := newStore(*storeKind, *out, log)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("preparing store: %w", err)
	}

	log.Info("polling",
		"source", adapter.Name(),
		"from", window.From.Format("2006-01-02"),
		"to", window.To.Format("2006-01-02"),
		"store", *storeKind,
		"destination", destination(st, *out))

	runner := &poll.Runner{Adapter: adapter, Hasher: hasher, Store: st, Log: log}
	res, runErr := runner.Run(ctx, window)

	// Report what happened even when the run failed partway — a backfill that stored twenty days
	// and then hit a rate limit has done twenty days of useful work, and the operator needs to
	// know that rather than assuming nothing landed.
	if res != nil && res.DaysPolled > 0 {
		lvl, msg, attrs := res.Diagnose()
		log.Log(ctx, lvl, msg, attrs...)
	}
	if runErr != nil {
		return runErr
	}

	fmt.Printf("\nStored %d records across %d days (%d distinct actors) in %s\n",
		res.Records, res.DaysWithData, len(res.Actors), destination(st, *out))
	if _, ok := st.(*clickhouse.Store); ok {
		fmt.Printf("Query it:\n"+
			"  SELECT * FROM %s.gold_fleet_adoption ORDER BY day\n", clickhouse.Database)
	} else {
		fmt.Printf("Query it with DuckDB:\n"+
			"  duckdb -c \"SELECT * FROM read_json_auto('%s/bronze/*/*.jsonl', hive_partitioning => true) LIMIT 5\"\n",
			strings.TrimRight(*out, "/\\"))
	}
	return nil
}

// runMigrate applies the ClickHouse schema and exits.
//
// `poll` already migrates on every run, and while the poller was the only writer that was
// enough. The collector path is different: the OpenTelemetry Collector's exporter runs with
// create_schema: false and only ever INSERTs (sql/clickhouse/004_bronze_collector.sql says why),
// so its tables have to exist before it starts. A deployment that uses the hook pack and never
// polls the Admin API would otherwise have no way to create them.
//
// It needs no salt and no admin key: it writes schema, never data, so there is no identity for it
// to handle.
func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "verbose logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	st, err := newStore("clickhouse", "", log)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("applying schema: %w", err)
	}
	fmt.Printf("Schema is current in %s\n", destination(st, ""))

	// The views over the collector's data read two deployment choices when they run (ADR-0029).
	// They are written here, not in a migration, because migrations are immutable and these are
	// not. An unset variable leaves the stored value alone, so running migrate by hand without
	// them changes nothing; a variable set to "" resets its setting to the default. The poller
	// never writes them.
	ch, ok := st.(*clickhouse.Store)
	if !ok {
		return errors.New("migrate needs the ClickHouse store")
	}
	vs := clickhouse.ViewSettings{
		OrgArtifacts: lookupEnv("CARDO_ORG_ARTIFACTS"),
		MinGroupSize: lookupEnv("CARDO_MIN_GROUP_SIZE"),
	}
	if err := ch.ApplyViewSettings(ctx, vs); err != nil {
		return err
	}
	org, size, err := ch.EffectiveViewSettings(ctx)
	if err != nil {
		return err
	}
	if org == "" {
		org = "(none named)"
	}
	fmt.Printf("Views: organization artifacts %s, minimum group size %d\n", org, size)
	return nil
}

func lookupEnv(key string) *string {
	if v, ok := os.LookupEnv(key); ok {
		return &v
	}
	return nil
}

// newHasher builds the pseudonymizer, refusing to start without a usable salt.
//
// Failing here is correct behaviour. A poller that ran with a weak or absent salt would produce a
// store that looks pseudonymous and is not, which is worse than one that does not run.
func newHasher() (*pseudonym.Hasher, error) {
	salt := os.Getenv("CARDO_SALT")
	if salt == "" {
		return nil, errors.New(
			"CARDO_SALT is not set.\n" +
				"  Cardo will not write a store without one: every actor identifier is hashed\n" +
				"  before storage and there is no unsalted mode (INV-2, ADR-0006).\n" +
				"  Generate one with:  openssl rand -hex 32\n" +
				"  Custody belongs with the security team, not with the analytics environment")
	}
	saltVersion := os.Getenv("CARDO_SALT_VERSION")
	if saltVersion == "" {
		saltVersion = "v1"
	}
	h, err := pseudonym.New(salt, saltVersion)
	if err != nil {
		return nil, fmt.Errorf("CARDO_SALT rejected: %w", err)
	}
	return h, nil
}

func newAdapter(name string, log *slog.Logger) (source.Adapter, error) {
	switch source.Name(name) {
	case source.Console:
		key := os.Getenv("CARDO_ADMIN_KEY")
		if key == "" {
			return nil, errors.New(
				"CARDO_ADMIN_KEY is not set.\n" +
					"  The Claude Code Analytics API needs an Admin API key (sk-ant-admin...),\n" +
					"  provisioned by an organization admin in Console > Settings > Admin keys.\n" +
					"  Individual accounts cannot reach this API; an organization is required")
		}
		return console.New(key, log)
	case source.Enterprise:
		return nil, errors.New(
			"the Claude Enterprise Analytics adapter is not built yet.\n" +
				"  It is in scope for v1 and is built second — see ADR-0021 and risks.md #8.\n" +
				"  Enterprise organizations use a different endpoint and an Analytics API key\n" +
				"  created by the primary owner in claude.ai, not an Admin API key")
	default:
		return nil, fmt.Errorf("unknown source %q; known sources: console", name)
	}
}

// resolveWindow turns flags into a day range, ending yesterday by default. Without -from it is
// the -days days ending at -to, or at yesterday when -to is not given.
func resolveWindow(now time.Time, from, to string, days int) (source.Window, error) {
	end := poll.DefaultWindow(now, 1).To
	if to != "" {
		t, err := time.Parse("2006-01-02", to)
		if err != nil {
			return source.Window{}, fmt.Errorf("parsing -to: %w", err)
		}
		end = t.UTC()
	}

	if from != "" {
		f, err := time.Parse("2006-01-02", from)
		if err != nil {
			return source.Window{}, fmt.Errorf("parsing -from: %w", err)
		}
		if f.After(end) {
			return source.Window{}, fmt.Errorf("-from %s is after -to %s",
				f.Format("2006-01-02"), end.Format("2006-01-02"))
		}
		return source.Window{From: f.UTC(), To: end}, nil
	}

	if days < 1 {
		return source.Window{}, fmt.Errorf("-days must be at least 1, got %d", days)
	}
	return source.Window{From: end.AddDate(0, 0, -(days - 1)), To: end}, nil
}

var (
	_ store.Store = (*jsonl.Store)(nil)
	_ store.Store = (*clickhouse.Store)(nil)
)

// newStore builds the storage target.
//
// ADR-0009 requires both to work. ClickHouse is the reference stack for a real deployment; the
// file store exists so that a five-engineer pilot can see the whole pipeline without asking
// anyone to approve infrastructure. Neither is a second-class path.
func newStore(kind, dir string, log *slog.Logger) (store.Store, error) {
	switch kind {
	case "file":
		return jsonl.New(dir)
	case "clickhouse":
		st, err := clickhouse.New(clickhouse.Config{
			Endpoint: os.Getenv("CARDO_CLICKHOUSE_URL"),
			User:     envOr("CARDO_CLICKHOUSE_USER", "default"),
			Password: os.Getenv("CARDO_CLICKHOUSE_PASSWORD"),
		}, log)
		if err != nil {
			return nil, err
		}
		// INV-7: no data leaves the organization's network. The analytics endpoint is pinned in code
		// and cannot be redirected, but the sink cannot be pinned — only the operator knows where
		// their own ClickHouse lives, and a tool that refused every address it had not been told
		// about would be a tool nobody could deploy.
		//
		// What is possible is to refuse to be quiet about it. Cardo cannot tell an internal
		// address from an external one by looking at it, so it says what it sees and leaves the
		// judgement where it belongs.
		if !st.IsPrivateDestination() {
			log.Warn("ClickHouse destination is not an obviously-internal address",
				"host", st.Host(),
				"check", "pseudonymous engineering telemetry will be written here; "+
					"confirm this is inside your own network (INV-7)")
		}
		return st, nil
	default:
		return nil, fmt.Errorf("unknown store %q; known stores: file, clickhouse", kind)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// destination describes where a store writes, for the log line and the closing summary. An
// operator should be able to read the destination of their engineering telemetry off the first
// line of output rather than inferring it from flags.
func destination(st store.Store, dir string) string {
	if ch, ok := st.(*clickhouse.Store); ok {
		return "clickhouse://" + ch.Host() + "/" + clickhouse.Database
	}
	return dir
}
