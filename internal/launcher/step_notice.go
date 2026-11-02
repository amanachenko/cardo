package launcher

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amanachenko/cardo/internal/notice"
)

// stepNotice writes the engineer notice: the one page an organization hands to its engineers.
//
// It reads the settings file, so the page describes the installation that exists and not a
// default. The page carries no secret and no address: the salt is named as an artefact, never
// printed, and a pattern that names the organization is reported as set without its text.
//
// Who holds the salt is the operator's own words in CARDO_SALT_HOLDER. During a pilot the operator
// creates and holds the salt (ADR-0034), so an empty key names the operator and nothing guesses a
// security team from the team path. The step asks the operator to fill the key in.
func stepNotice(opts Options) (Result, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		return Result{
			ID: "notice", Name: "NOTICE", Value: "cannot read the settings file",
			Cause: fmt.Sprintf("The settings file at %s could not be read: %v", filepath.Join(opts.Dir, ".env"), err),
			Fix:   "Run cardo up first, so the settings file exists.",
		}, nil
	}

	page := notice.FromEnv(env).Markdown()

	path := filepath.Join(opts.Dir, "engineer-notice.md")
	if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
		return Result{
			ID: "notice", Name: "NOTICE", Value: "cannot write the notice",
			Cause: fmt.Sprintf("Writing %s failed: %v", path, err),
			Fix:   "Check that the directory is writable and that no other process holds the file.",
		}, nil
	}
	value := path + " — salt custody, the settings in force, and what the data is not used for"
	if env["CARDO_SALT_HOLDER"] == "" {
		value += " — set CARDO_SALT_HOLDER in .env to say who holds the salt"
	}
	return Result{
		ID: "notice", Name: "NOTICE",
		Value: value,
		OK:    true,
	}, nil
}
