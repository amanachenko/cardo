package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/coach"
	"github.com/amanachenko/cardo/internal/launcher"
	"github.com/amanachenko/cardo/internal/pseudonym"
)

// runCoach prints one person's figures against their own previous window. It is the CLI twin of the
// coach page and reads the same views; it never shows another person and never ranks anyone.
func runCoach(args []string) error {
	fs := flag.NewFlagSet("coach", flag.ContinueOnError)
	useSynopsis(fs, "coach")
	dir := fs.String("dir", filepath.Join("deploy", "compose"), "stack directory that holds .env")
	days := fs.Int("days", 28, "window length in days")
	version := fs.String("version", "v1", "salt version")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	if *days < 7 || *days > 365 {
		return fmt.Errorf("days must be between 7 and 365")
	}

	env, err := launcher.ReadDotEnv(filepath.Join(*dir, ".env"))
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Join(*dir, ".env"), err)
	}
	id, err := pseudonym.ForEmail(env["CARDO_SALT"], *version, fs.Arg(0))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	endpoint := env["CARDO_CLICKHOUSE_URL"]
	if endpoint == "" {
		endpoint = "http" + "://" + "127.0.0.1:8124"
	}
	user := env["CLICKHOUSE_USER"]
	pass := env["CLICKHOUSE_PASSWORD"]

	to := time.Now().UTC()
	from := to.AddDate(0, 0, -(*days - 1))
	prevTo := from.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -(*days - 1))

	now, arts, err := coach.Query(ctx, endpoint, user, pass, id.Value, from, to)
	if err != nil {
		return err
	}
	prev, _, err := coach.Query(ctx, endpoint, user, pass, id.Value, prevFrom, prevTo)
	if err != nil {
		return err
	}
	untried, err := coach.Untried(ctx, endpoint, user, pass, id.Value, from, to)
	if err != nil {
		return err
	}

	r := coach.Report{Pseudonym: id.Value, From: from, To: to, PrevFrom: prevFrom, Now: now, Prev: prev, Artifacts: arts, Untried: coach.Names(untried)}
	r.Suggestions = coach.Suggest(untried)

	if *asJSON {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("COACH · %s… · last %d days\n\n", short(id.Value), *days)
	if now.Sessions == 0 {
		if n, _ := coach.Unidentified(ctx, endpoint, user, pass, from, to); n > 0 {
			fmt.Printf("No sessions for this pseudonym, but %d arrived without an identity.\n", n)
			fmt.Println("Claude Code omits user.email when a run authenticates with an API key; sign in with the Claude account, or label the run as a service.")
		} else {
			fmt.Println("No sessions for this pseudonym in this window. Run Claude Code once in an instrumented project.")
		}
		return nil
	}
	line := func(label string, value, prevValue string) {
		fmt.Printf("%-16s %-14s %s\n", label, value, prevValue)
	}
	line("Sessions", itoa(now.Sessions), "prev "+itoa(prev.Sessions))
	line("Prompts", itoa(now.Prompts), "prev "+itoa(prev.Prompts))
	line("Cost", fmt.Sprintf("$%.2f", now.CostUSD), fmt.Sprintf("prev $%.2f", prev.CostUSD))
	line("Peak context", fmt.Sprintf("%d", now.PeakContext), fmt.Sprintf("prev %d", prev.PeakContext))
	// A window of imported history carries no tool decisions, so the figures were never looked
	// for. Saying "0 failed" there would claim a measurement nobody made (ADR-0050).
	tools := "not observed"
	prevTools := ""
	if now.ToolsObserved {
		tools = fmt.Sprintf("%d (%d failed)", now.ToolCalls, now.FailedTools)
		prevTools = fmt.Sprintf("prev %d", prev.ToolCalls)
	}
	line("Tool calls", tools, prevTools)
	if len(arts) > 0 {
		names := make([]string, 0, len(arts))
		for _, a := range arts {
			names = append(names, fmt.Sprintf("%s×%d", a.Name, a.Loads))
		}
		line("Artifacts", strings.Join(names, ", "), "")
	}
	if len(r.Suggestions) == 0 {
		fmt.Println("\nNo advice this window: the figures moved within their usual range.")
		return nil
	}
	fmt.Println("\nSUGGESTIONS")
	for i, s := range r.Suggestions {
		fmt.Printf("%d. %s\n", i+1, s)
	}
	fmt.Println("\nCompared with your own previous window. No rank, no percentile.")
	return nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
