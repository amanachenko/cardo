package launcher

import "strings"

// The ACL reading below is shared by every platform's test, because it is where a wrong reading
// turns into a false answer. The Windows file calls it; the test exercises it everywhere.

// accessEntries reads the access entries out of icacls' output. The first line is the path of the
// file, and the last is a summary of what was processed; neither names an account.
func accessEntries(out string) []string {
	var entries []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isSummaryLine(line) {
			continue
		}
		entries = append(entries, line)
	}
	if len(entries) > 0 {
		entries = entries[1:] // the path of the file
	}
	return entries
}

// principalOf reads the account an ACL line names. A line looks like "    DOMAIN\User:(F)": the
// account, then the permissions. The name may itself contain a backslash, so it is taken whole up
// to the colon, and only a line with no permissions at all is read as it stands.
func principalOf(line string) (string, bool) {
	if i := strings.Index(line, ":"); i > 0 {
		p := strings.TrimSpace(line[:i])
		if p == "" {
			return "", false
		}
		return p, true
	}
	return line, true
}

// isSummaryLine reports whether a line is icacls' closing summary rather than an access entry.
func isSummaryLine(line string) bool {
	return strings.HasPrefix(line, "Successfully processed")
}

// isOwnerEntry reports whether the ACL entry names the account that owns the file. The grant may
// name the bare account while whoami names it with its domain, so both spellings count.
func isOwnerEntry(who string, owners []string) bool {
	for _, o := range owners {
		if strings.EqualFold(who, o) {
			return true
		}
		if strings.HasSuffix(strings.ToUpper(who), "\\"+strings.ToUpper(o)) {
			return true
		}
	}
	return false
}

// isSystemEntry reports whether the ACL entry names a group that reads any file through its own
// privileges, rather than through access granted to this file.
func isSystemEntry(who string) bool {
	switch strings.ToUpper(who) {
	case "BUILTIN\\ADMINISTRATORS", "NT AUTHORITY\\SYSTEM", "BUILTIN\\SYSTEM", "CREATOR OWNER":
		return true
	}
	return false
}
