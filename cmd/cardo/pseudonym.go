package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amanachenko/cardo/internal/launcher"
	"github.com/amanachenko/cardo/internal/pseudonym"
)

// runPseudonym prints the value the views join on. It reads the same salt the collector uses, so
// the answer matches what telemetry already produced; the email itself is never written anywhere.
func runPseudonym(args []string) error {
	fs := flag.NewFlagSet("pseudonym", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), pseudonymUsage) }
	useSynopsis(fs, "pseudonym")
	dir := fs.String("dir", filepath.Join("deploy", "compose"), "stack directory that holds .env")
	salt := fs.String("salt", "", "salt to use instead of CARDO_SALT or .env")
	version := fs.String("version", "v1", "salt version the pseudonym belongs to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}

	key := *salt
	if key == "" {
		key = os.Getenv("CARDO_SALT")
	}
	if key == "" {
		env, err := launcher.ReadDotEnv(filepath.Join(*dir, ".env"))
		if err == nil {
			key = env["CARDO_SALT"]
		}
	}
	id, err := pseudonym.ForEmail(key, *version, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Printf("PSEUDONYM  %s\nSALT       %s\n", id.Value, id.Version)
	return nil
}

const pseudonymUsage = `Print the pseudonym the views join on.

Usage:
  cardo pseudonym [-dir deploy/compose] [-version v1] [-salt <hex>] <email>

The salt comes from -salt, then CARDO_SALT, then <dir>/.env. The address is hashed in memory and
never printed or stored; the value matches what the collector wrote for the same salt.`
