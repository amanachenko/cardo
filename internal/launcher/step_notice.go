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
// The pilot path is the one where the security team holds the salt (ADR-0034), so the page says
// so when the launcher is running the pilot rows.
func stepNotice(opts Options) (Result, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		return Result{
			ID: "notice", Name: "NOTICE", Value: "cannot read the settings file",
			Cause: fmt.Sprintf("The settings file at %s could not be read: %v", filepath.Join(opts.Dir, ".env"), err),
			Fix:   "Run cardo up first, so the settings file exists.",
		}, nil
	}

	custody := notice.CustodyOperator
	if opts.Pilot {
		custody = notice.CustodySecurityTeam
	}
	page := notice.FromEnv(env, custody).Markdown()

	path := filepath.Join(opts.Dir, "engineer-notice.md")
	if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
		return Result{
			ID: "notice", Name: "NOTICE", Value: "cannot write the notice",
			Cause: fmt.Sprintf("Writing %s failed: %v", path, err),
			Fix:   "Check that the directory is writable and that no other process holds the file.",
		}, nil
	}
	return Result{
		ID: "notice", Name: "NOTICE",
		Value: path + " — salt custody, the settings in force, and what the data is not used for",
		OK:    true,
	}, nil
}
