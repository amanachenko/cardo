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
// collector at most a second.
func runBundle(args []string) error {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	useSynopsis(fs, "bundle")
	var (
		dir   = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
		pilot = fs.Bool("pilot", false, "the bundle points at the collector's network address, not loopback")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	res, err := launcher.RunStep(context.Background(), launcher.Options{Dir: *dir, Pilot: *pilot, Out: os.Stdout}, "bundle")
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
// document, a chat or a file.
func runNotice(args []string) error {
	fs := flag.NewFlagSet("notice", flag.ContinueOnError)
	useSynopsis(fs, "notice")
	var (
		dir   = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
		out   = fs.String("out", "", "write the page to this file instead of stdout")
		pilot = fs.Bool("pilot", false, "the security team holds the salt, not the operator")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}

	env, err := launcher.ReadDotEnv(filepath.Join(*dir, ".env"))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no settings file at %s: run cardo up first", filepath.Join(*dir, ".env"))
		}
		return fmt.Errorf("cannot read %s: %w", filepath.Join(*dir, ".env"), err)
	}

	custody := notice.CustodyOperator
	if *pilot {
		custody = notice.CustodySecurityTeam
	}
	page := notice.FromEnv(env, custody).Markdown()

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
