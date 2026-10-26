package pseudonym

import (
	"strings"
	"testing"
)

const testSaltID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestForEmailIsStableAndCaseInsensitive(t *testing.T) {
	a, err := ForEmail(testSaltID, "v1", "Developer@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ForEmail(testSaltID, "v1", "  developer@example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if a.Value != b.Value {
		t.Fatalf("expected the same pseudonym, got %q and %q", a.Value, b.Value)
	}
	if len(a.Value) < 32 || strings.Contains(a.Value, "@") {
		t.Fatalf("not a pseudonym: %q", a.Value)
	}
	if a.Version != "v1" {
		t.Fatalf("version not carried: %q", a.Version)
	}
}

func TestForEmailChangesWithSaltOrVersion(t *testing.T) {
	a, _ := ForEmail(testSaltID, "v1", "developer@example.com")
	b, _ := ForEmail(strings.Repeat("ab", 32), "v1", "developer@example.com")
	c, _ := ForEmail(testSaltID, "v2", "developer@example.com")
	if a.Value == b.Value {
		t.Fatal("different salts produced the same pseudonym")
	}
	if a.Value != c.Value {
		t.Fatal("the salt version must not change the hash for the same salt")
	}
}

func TestForEmailRejectsInput(t *testing.T) {
	for _, email := range []string{"", "developer", "developer@", "@example.com", "developer@example"} {
		if _, err := ForEmail(testSaltID, "v1", email); err == nil {
			t.Fatalf("%q was accepted", email)
		}
	}
	if _, err := ForEmail("", "v1", "developer@example.com"); err == nil {
		t.Fatal("empty salt was accepted")
	}
}
