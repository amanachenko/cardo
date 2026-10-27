package launcher

import (
	"strings"
	"testing"
)

// A real icacls reading, as the Windows runner produces it. The first line is the path, the last
// is the summary; the entries in between name accounts.
const icaclsOutput = `C:\Users\runneradmin\AppData\Local\Temp\Test\001\.env
    WORKGROUP\runneradmin:(F)
    BUILTIN\Administrators:(F)
    NT AUTHORITY\SYSTEM:(F)

Successfully processed 1 files; Failed processing 0 files
`

// The path of the file and the closing summary are not accounts. Reading the path as one would
// report "C" as somebody who can read the file, and the summary as another.
func TestAccessEntriesSkipsThePathAndTheSummary(t *testing.T) {
	entries := accessEntries(icaclsOutput)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %q", len(entries), entries)
	}
	for _, e := range entries {
		if strings.Contains(e, "AppData") || strings.HasPrefix(e, "Successfully") {
			t.Errorf("not an access entry: %q", e)
		}
	}
}

// An account name may contain a backslash, and the permissions follow a colon. Reading the name up
// to the backslash would leave "NT AUTHORITY" behind and read it as a different account.
func TestPrincipalOfReadsTheWholeAccount(t *testing.T) {
	cases := map[string]string{
		"    WORKGROUP\\runneradmin:(F)":  "WORKGROUP\\runneradmin",
		"    BUILTIN\\Administrators:(F)": "BUILTIN\\Administrators",
		"    NT AUTHORITY\\SYSTEM:(F)":    "NT AUTHORITY\\SYSTEM",
		"    CREATOR OWNER:(I)":           "CREATOR OWNER",
	}
	for line, want := range cases {
		got, ok := principalOf(line)
		if !ok {
			t.Errorf("%q: not read", line)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", line, got, want)
		}
	}
}

// The owner may be named with its domain or bare, and the grant may name it either way.
func TestOwnerIsRecognisedInEitherSpelling(t *testing.T) {
	owners := []string{"WORKGROUP\\runneradmin", "runneradmin"}
	for _, who := range []string{"WORKGROUP\\runneradmin", "runneradmin"} {
		if !isOwnerEntry(who, owners) {
			t.Errorf("%q is not recognised as the owner", who)
		}
	}
	if isOwnerEntry("BUILTIN\\Users", owners) {
		t.Error("the local users are not the owner")
	}
}

// Administrators and SYSTEM read any file through their own privileges. That is a property of
// Windows, not of this file, and it does not make the secret readable by the people it was meant
// for.
func TestSystemGroupsAreNotSomebodyElse(t *testing.T) {
	for _, who := range []string{"BUILTIN\\Administrators", "NT AUTHORITY\\SYSTEM", "CREATOR OWNER"} {
		if !isSystemEntry(who) {
			t.Errorf("%q is not treated as a system group", who)
		}
	}
	// The groups that would actually leak it are not system groups.
	for _, who := range []string{"Everyone", "BUILTIN\\Users", "NT AUTHORITY\\Authenticated Users", "BUILTIN\\Guests"} {
		if isSystemEntry(who) {
			t.Errorf("%q must not be treated as a system group", who)
		}
	}
}
