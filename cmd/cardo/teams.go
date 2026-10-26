package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/amanachenko/cardo/internal/directory"
	"github.com/amanachenko/cardo/internal/launcher"
)

// printJSON writes a result as JSON, for a script that reads it.
func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// runTeams reads a team directory and says what it would do.
//
// The directory is the operator's file: it maps an address to a team, and Cardo keeps only the
// hashed mapping (ADR-0046). The command shows the counts before anything is written, and it
// writes nothing unless --apply is given.
//
// It never prints an address. The preview is counts — rows, teams, and how many of those teams
// are too small to be shown at all — because a directory of people is exactly what this product
// refuses to become.
func runTeams(args []string) error {
	fs := flag.NewFlagSet("teams", flag.ContinueOnError)
	useSynopsis(fs, "teams")
	var (
		dir    = fs.String("dir", filepath.Join("deploy", "compose"), "stack directory that holds .env")
		file   = fs.String("file", "", "the directory file to read (CSV: email,team)")
		apply  = fs.Bool("apply", false, "write the mapping; without it, nothing is written")
		asJSON = fs.Bool("json", false, "print the result as JSON")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	if *file == "" {
		return fmt.Errorf("teams needs --file <directory.csv>: the file that maps each address to a team")
	}

	text, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", *file, err)
	}

	env, err := launcher.ReadDotEnv(filepath.Join(*dir, ".env"))
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Join(*dir, ".env"), err)
	}
	salt := env["CARDO_SALT"]
	version := env["CARDO_SALT_VERSION"]
	if version == "" {
		version = "v1"
	}

	entries, preview := directory.Parse(string(text))
	floor := 5
	if n, err := strconv.Atoi(env["CARDO_MIN_GROUP_SIZE"]); err == nil && n >= 5 {
		floor = n
	}
	below := directory.BelowFloor(preview, floor)

	out := map[string]any{
		"rows":       preview.Rows,
		"unique":     preview.Unique,
		"duplicates": preview.Duplicates,
		"teams":      preview.Teams,
		"belowFloor": below,
		"floor":      floor,
		"malformed":  preview.Malformed,
		"written":    false,
	}

	if len(preview.Malformed) > 0 {
		out["problem"] = "fix the malformed lines first; nothing was written"
		if *asJSON {
			return printJSON(out)
		}
		fmt.Println("TEAMS · nothing written")
		for _, p := range preview.Malformed {
			fmt.Printf("  line %d: %s\n", p.Line, p.Reason)
		}
		return fmt.Errorf("the file has %d malformed line(s); nothing was written", len(preview.Malformed))
	}
	if len(entries) == 0 {
		return fmt.Errorf("the file has no rows")
	}

	if !*apply {
		out["problem"] = "dry run: nothing was written; pass --apply to store the mapping"
		if *asJSON {
			return printJSON(out)
		}
		fmt.Printf("TEAMS · %d people in %d teams · dry run, nothing written\n", preview.Unique, preview.Teams)
		if below > 0 {
			fmt.Printf("  %d team(s) below the floor of %d: they stay hidden\n", below, floor)
		}
		fmt.Println("  pass --apply to store the mapping")
		return nil
	}

	// Hashing happens here and nowhere else: the address is turned into the same code name the
	// collector produces, and the file itself is never stored (ADR-0046).
	hashed, err := launcher.HashDirectory(entries, salt, version)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	endpoint := env["CARDO_CLICKHOUSE_URL"]
	if endpoint == "" {
		endpoint = "http" + "://" + "127.0.0.1:8124"
	}
	if _, err := launcher.WriteTeams(ctx, launcher.TeamsWrite{
		Endpoint:    endpoint,
		User:        env["CLICKHOUSE_USER"],
		Password:    env["CLICKHOUSE_PASSWORD"],
		SaltVersion: version,
		Rows:        hashed,
		SourceHash:  launcher.SourceHashOf(string(text)),
		Teams:       preview.Teams,
	}); err != nil {
		return err
	}

	out["written"] = true
	if *asJSON {
		return printJSON(out)
	}
	fmt.Printf("TEAMS · %d people in %d teams · written\n", preview.Unique, preview.Teams)
	if below > 0 {
		fmt.Printf("  %d team(s) below the floor of %d: they stay hidden\n", below, floor)
	}
	return nil
}
