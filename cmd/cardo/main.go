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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/amanachenko/cardo/internal/poll"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/retention"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/source/console"
	"github.com/amanachenko/cardo/internal/store"
	"github.com/amanachenko/cardo/internal/store/clickhouse"
	"github.com/amanachenko/cardo/internal/store/jsonl"
)

var version = "0.1.0-dev"

const usage = `cardo — installation, checks, and local telemetry for Claude Code

Usage:
  cardo <command> [flags]

Install
  up          Install or repair the reference stack, then prove that data arrives
  ui          Serve the board on loopback; its buttons run the same steps as up
  connect     Write .claude/settings.json into one project (--project <dir>)
  secret      Generate the salt and the passwords into .env with mode 0600

Check
  check       Read-only: run the same checks and exit 1 if anything is wrong
  probe       Send one OTLP log and one hook event, then read them back
  session     Check whether a real Claude Code session has reached the stack
  gold        Create the gold-only ClickHouse user and prove bronze/silver are refused
  pseudonym   Print the pseudonym the views join on (<email>)
  coach       Show your figures against your own previous window (<email>)

Data
  poll        Poll an analytics source into a local store
  mock-admin  Serve the Analytics API shape on loopback, for verification
  prune       Delete raw rows older than the retention window
  migrate     Apply the ClickHouse schema and exit

Other
  version     Print the version (--long adds platform and toolchain)
  help        Show this text, or "help <command>" for one command

Examples
  cardo up
  cardo ui --no-open
  cardo connect --project ~/work/api
  cardo check

Exit codes
  0  success
  1  a check failed, or the command could not finish
  2  usage error

Secrets come from the environment, never from flags: command-line arguments are
visible to every other process on the machine.

  CARDO_ADMIN_KEY      Admin API key, provisioned by an organization admin
  CARDO_SALT           Pseudonymization salt, at least 32 hex characters
  CARDO_SALT_VERSION   Label for the current salt (default: v1)
`

// parsed keeps flag errors quiet: the flag package already printed the reason, so the process should
// exit 2 without a second copy of the same sentence. -h is not an error.
func parsed(err error) error {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return errUsage
}

// errUsage marks a bad invocation: the message is already the help text, so main exits 2 without
// printing a second line that repeats what the person just read.
var errUsage = errors.New("usage error")

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	if errors.Is(err, errUsage) {
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "\ncardo: %v\n", err)
	os.Exit(1)
}

// synopsis is the one line a person needs before typing the command, and the flag text comes from
// the command itself so the two can never disagree.
var synopsis = map[string][2]string{
	"up":      {"cardo up [flags]", "Install or repair the reference stack, then prove that data arrives."},
	"ui":      {"cardo ui [flags]", "Serve the board on loopback; its buttons run the same steps."},
	"check":   {"cardo check [flags]", "Read-only checks; exit 1 if anything fails."},
	"connect": {"cardo connect --project <dir>", "Write .claude/settings.json into one project."},
	"secret":  {"cardo secret [--dir]", "Generate the salt and passwords into .env (mode 0600)."},
	"probe":   {"cardo probe [--dir]", "Send one OTLP log and one hook event, then read them back."},
	"session": {"cardo session [--dir]", "Check whether a real session has reached the stack."},
	"gold":    {"cardo gold [--dir]", "Create the gold-only ClickHouse user and prove the refusal."},
	"poll":    {"cardo poll [flags]", "Poll an analytics source into a local store."},
	"migrate": {"cardo migrate [-v]", "Apply the ClickHouse schema and exit."},
	"version": {"cardo version [--long|--short]", "Print the version; --long adds platform and toolchain, --short prints the bare version."},
}

// helpFor prints one command's synopsis instead of the whole surface.
func helpFor(cmd string) error {
	s, ok := synopsis[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "cardo: no help for %q\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		return errUsage
	}
	fmt.Printf("%s\n\n%s\n\nRun \"cardo %s -h\" for every flag.\n", s[0], s[1], cmd)
	return nil
}

// useSynopsis puts the one-line meaning above the flag list, so -h never opens with "Usage of up:".
func useSynopsis(fs *flag.FlagSet, cmd string) {
	fs.Usage = func() {
		if v, ok := synopsis[cmd]; ok {
			fmt.Println(v[0])
			fmt.Println()
			fmt.Println(v[1])
			fmt.Println()
		}
		fs.PrintDefaults()
	}
}

// runVersion prints the mark and the version. The two blocks are the product mark: amber for what is
// done, dim for what is still ahead; the same pair the board uses.
func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	useSynopsis(fs, "version")
	long := fs.Bool("long", false, "add platform and toolchain")
	short := fs.Bool("short", false, "print only the version, for scripts")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	if *short {
		fmt.Println(version)
		return nil
	}
	done, ahead := "\u2588", "\u2588"
	if isTTY(os.Stdout) && os.Getenv("NO_COLOR") == "" {
		done = "\x1b[38;2;215;161;42m" + done + "\x1b[0m"
		ahead = "\x1b[38;2;169;164;148m" + ahead + "\x1b[0m"
	}
	fmt.Printf("%s %s  cardo %s\n", done, ahead, version)
	if *long {
		fmt.Printf("        %s/%s · %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	}
	return nil
}

// isTTY keeps colour out of pipes and CI logs.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return errUsage
	}

	switch args[0] {
	case "up":
		return runUp(args[1:])
	case "check":
		return runCheck(args[1:])
	case "ui":
		return runServe(args[1:])
	case "connect":
		return runConnect(args[1:])
	case "secret":
		return runSecret(args[1:])
	case "probe":
		return runStepCommand("data", args[1:])
	case "session":
		return runStepCommand("session", args[1:])
	case "gold":
		return runStepCommand("gold", args[1:])
	case "pseudonym":
		return runPseudonym(args[1:])
	case "import":
		return runImport(args[1:])
	case "coach":
		return runCoach(args[1:])
	case "poll":
		return runPoll(args[1:])
	case "mock-admin":
		return runMockAdmin(args[1:])
	case "prune":
		return runPrune(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "version":
		return runVersion(args[1:])
	case "-h", "--help":
		fmt.Print(usage)
		return nil
	case "help":
		if len(args) > 1 {
			return helpFor(args[1])
		}
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "cardo: unknown command %q\n\n", args[0])
		fmt.Fprint(os.Stderr, usage)
		return errUsage
	}
}

func runPoll(args []string) error {
	fs := flag.NewFlagSet("poll", flag.ContinueOnError)
	useSynopsis(fs, "poll")
	var (
		srcName   = fs.String("source", "console", "analytics source: console")
		storeKind = fs.String("store", "file", "storage target: file | clickhouse")
		out       = fs.String("out", "./data", "directory for the local store (-store file)")
		days      = fs.Int("days", 30, "number of days to poll, ending at -to")
		from      = fs.String("from", "", "first day to poll, YYYY-MM-DD (overrides -days)")
		to        = fs.String("to", "", "last day to poll, YYYY-MM-DD (defaults to yesterday)")
		verbose   = fs.Bool("v", false, "verbose logging")
		every     = fs.Duration("every", 0, "run continuously with this interval (service mode); 0 runs once")
		baseURL   = fs.String("base-url", "", "local stand-in for the Admin API; loopback only (development)")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
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

	base := ""
	if *baseURL != "" {
		base, err = console.LoopbackBase(*baseURL)
		if err != nil {
			return err
		}
		log.Warn("using a local Admin API stand-in", "base", base)
	}
	adapter, err := newAdapter(*srcName, log, base)
	if err != nil {
		// In service mode a missing key means "the organization has not enabled the Admin API
		// path yet", not a broken container. Say so once and exit cleanly instead of restarting.
		if *every > 0 && strings.Contains(err.Error(), "CARDO_ADMIN_KEY") {
			log.Warn("polling disabled: CARDO_ADMIN_KEY is not set")
			return nil
		}
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
	if *every > 0 {
		return pollLoop(ctx, runner, st, log, *every, *days, *out, *storeKind)
	}
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

// pollLoop is the service mode: poll the last two days (late rows are common), then sleep. The
// window is recomputed every time so a long-running container does not drift.
func pollLoop(ctx context.Context, runner *poll.Runner, st store.Store, log *slog.Logger, every time.Duration, days int, out, storeKind string) error {
	if days < 2 {
		days = 2
	}
	for {
		window, err := resolveWindow(time.Now(), "", "", days)
		if err != nil {
			return err
		}
		res, runErr := runner.Run(ctx, window)
		if res != nil && res.DaysPolled > 0 {
			lvl, msg, attrs := res.Diagnose()
			log.Log(ctx, lvl, msg, attrs...)
		}
		if runErr != nil {
			log.Error("poll failed; will retry", "error", runErr)
		}
		// Zero means "keep everything", and that is a choice too: the pass takes the tables' TTL off
		// so nothing expires on its own (ADR-0053).
		if days := retentionFromEnv(); days >= 0 && runErr == nil {
			if rep, err := retention.Prune(ctx, os.Getenv("CARDO_CLICKHOUSE_URL"), envOr("CARDO_CLICKHOUSE_USER", "default"), os.Getenv("CARDO_CLICKHOUSE_PASSWORD"), days, false); err != nil {
				log.Warn("retention pass failed", "error", err)
			} else {
				log.Info("retention pass", "days", days, "deleted", rep)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(every):
		}
	}
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
	useSynopsis(fs, "migrate")
	verbose := fs.Bool("v", false, "verbose logging")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
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

func newAdapter(name string, log *slog.Logger, base string) (source.Adapter, error) {
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
		if base != "" {
			return console.New(key, log, console.WithBaseURL(base))
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
