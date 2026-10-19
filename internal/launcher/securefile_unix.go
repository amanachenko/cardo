//go:build !windows

package launcher

import "os"

// restrictFile makes a secrets file readable only by its owner.
func restrictFile(path string) error {
	return os.Chmod(path, 0o600)
}

// RestrictFile is the exported form, for callers outside this package (the CLI's token file).
func RestrictFile(path string) error { return restrictFile(path) }
