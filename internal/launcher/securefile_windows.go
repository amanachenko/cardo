//go:build windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
)

// restrictFile removes inherited access and grants the current user full control, which is the
// Windows equivalent of mode 0600. NTFS has no numeric modes, so this is an ACL operation; if it
// fails, the caller must refuse to leave the file in place rather than write a readable secret.
func restrictFile(path string) error {
	name := os.Getenv("USERNAME")
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if name == "" {
		return fmt.Errorf("cannot determine the current user for %s", path)
	}

	if out, err := exec.Command("icacls", path, "/inheritance:r").CombinedOutput(); err != nil {
		return fmt.Errorf("icacls inheritance: %v: %s", err, out)
	}
	if out, err := exec.Command("icacls", path, "/grant:r", name+":F").CombinedOutput(); err != nil {
		return fmt.Errorf("icacls grant: %v: %s", err, out)
	}
	return nil
}

// RestrictFile is the exported form, for callers outside this package.
func RestrictFile(path string) error { return restrictFile(path) }
