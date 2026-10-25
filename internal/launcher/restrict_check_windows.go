//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"strings"
)

// restrictedToOwner reports whether only the owner can read the file, and why not when it cannot.
//
// NTFS has no numeric modes: the restriction is an ACL, so it is read back with icacls. The file
// must name its owner with access, and nobody else may read it.
//
// Two kinds of entry are not "somebody else". The owner of the file may keep access to it, and so
// may the local Administrators group and SYSTEM: an administrator reads any file on the machine
// through their own privileges, and that is a property of Windows, not of this file. What must not
// be there is a grant to everybody, to the local users, to guests, or to any signed-in user — those
// are the ones that make a secret readable by people it was not meant for.
func restrictedToOwner(path string) (bool, string) {
	out, err := exec.Command("icacls", path).CombinedOutput()
	if err != nil {
		return false, "icacls: " + err.Error()
	}
	owners := ownerNames()
	owner := false
	entries := 0
	for _, line := range accessEntries(string(out)) {
		who, ok := principalOf(line)
		if !ok {
			continue
		}
		entries++
		if isOwnerEntry(who, owners) {
			owner = true
			continue
		}
		if isSystemEntry(who) {
			continue
		}
		return false, "someone else can read it: " + who
	}
	if entries == 0 {
		return false, "icacls reported no access entries"
	}
	if !owner {
		return false, "the file does not grant access to its owner"
	}
	return true, ""
}

// ownerNames is every spelling the owner may appear under: the account with its domain, the bare
// account, and the one the environment gives.
func ownerNames() []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		out = append(out, s)
	}
	if who, err := exec.Command("whoami").Output(); err == nil {
		full := strings.TrimSpace(string(who))
		add(full)
		if i := strings.LastIndex(full, "\\"); i >= 0 {
			add(full[i+1:])
		}
	}
	add(os.Getenv("USERNAME"))
	return out
}
