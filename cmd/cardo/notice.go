package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amanachenko/cardo/internal/launcher"
	"github.com/amanachenko/cardo/internal/notice"
)

// runBundle writes the managed-settings bundle: the file an organization hands to the machines it
// manages.
//
// It is the same step the board runs, exposed for scripts and for machines where the board is not
// used. The bundle configures telemetry and nothing else, and every hook in it waits for the
// collector at most a second. Whether the bundle points at loopback or at the network address is
// decided by CARDO_HOSTNAME and by the path the organization chose in the board, both read from
// the same places the board reads them — there is no flag to disagree with.
func runBundle(args []string) error {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	useSynopsis(fs, "bundle")
	var (
		dir = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	res, err := launcher.RunStep(context.Background(), launcher.Options{Dir: *dir, Out: os.Stdout}, "bundle")
	if err != nil {
		return err
	}
	fmt.Printf("  %s: %s\n", res.Name, res.Value)
	if !res.OK {
		if res.Cause != "" {
			fmt.Printf("  cause: %s\n", res.Cause)
		}
		return fmt.Errorf("%s", res.Fix)
	}
	return nil
}

// runNotice writes the engineer notice: the one page an organization hands to its engineers.
//
// It reads the stack's .env, so the page describes the installation that exists and not a default.
// The page carries no secret and no address, and it goes to stdout so it can be piped into a
// document, a chat or a file. The only format is markdown.
func runNotice(args []string) error {
	fs := flag.NewFlagSet("notice", flag.ContinueOnError)
	useSynopsis(fs, "notice")
	var (
		dir    = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
		out    = fs.String("out", "", "write the page to this file instead of stdout")
		format = fs.String("format", "md", "output format; the only one is md")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	if *format != "md" {
		return fmt.Errorf("notice: --format %q is not supported; the only format is md", *format)
	}

	env, err := launcher.ReadDotEnv(filepath.Join(*dir, ".env"))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no settings file at %s: run cardo up first", filepath.Join(*dir, ".env"))
		}
		return fmt.Errorf("cannot read %s: %w", filepath.Join(*dir, ".env"), err)
	}

	page := notice.FromEnv(env).Markdown()

	if *out == "" {
		fmt.Print(page)
		return nil
	}
	if err := os.WriteFile(*out, []byte(page), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", *out, err)
	}
	fmt.Printf("  NOTICE: written to %s\n", *out)
	return nil
}
