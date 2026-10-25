//go:build windows

package launcher

import (
	"os/exec"
	"strings"
)

// restrictedToOwner reports whether only the owner can read the file, and why not when it cannot.
//
// NTFS has no numeric modes: the restriction is an ACL, so it is read back with icacls. The file
// must name the current user and nobody else — no inherited entry, no group, no "Everyone".
func restrictedToOwner(path string) (bool, string) {
	out, err := exec.Command("icacls", path).CombinedOutput()
	if err != nil {
		return false, "icacls: " + err.Error()
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return false, "icacls reported no access entries"
	}
	user := currentUser()
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		who := line
		if i := strings.Index(line, ":"); i > 0 {
			who = line[:i]
		} else if i := strings.Index(line, "\\"); i > 0 {
			who = line[:i]
		}
		who = strings.TrimSpace(who)
		if who == "" {
			continue
		}
		if !strings.EqualFold(who, user) && !strings.HasSuffix(strings.ToUpper(who), "\\"+strings.ToUpper(user)) {
			return false, "someone else can read it: " + who
		}
	}
	return true, ""
}

// currentUser is the account the file was granted to.
func currentUser() string {
	if out, err := exec.Command("whoami").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}
