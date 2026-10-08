package pseudonym

import (
	"strings"
	"testing"
)

const testSalt = "0123456789abcdef0123456789abcdef" // 32 chars, the documented minimum

func TestNewRejectsWeakSalt(t *testing.T) {
	for _, tc := range []struct {
		name, salt, version string
		wantErr             error
	}{
		{"empty", "", "v1", ErrSaltTooShort},
		{"one short", strings.Repeat("a", MinSaltLen-1), "v1", ErrSaltTooShort},
		{"not hex", strings.Repeat("z", MinSaltLen), "v1", ErrSaltNotHex},
		{"a passphrase", "correct horse battery staple, twice over", "v1", ErrSaltNotHex},
		{"no version", testSalt, "", ErrNoVersion},
		{"blank version", testSalt, "   ", ErrNoVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.salt, tc.version); err != tc.wantErr {
				t.Fatalf("New() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// A weak salt over an email address space is effectively no salt at all: the identifiers are
// low-entropy and guessable, so the pseudonyms would be reversible by anyone holding them.
// Refusing to start is the behaviour ADR-0006 asks for.
func TestNewAcceptsExactMinimum(t *testing.T) {
	if _, err := New(strings.Repeat("a", MinSaltLen), "v1"); err != nil {
		t.Fatalf("New() with a %d character salt: %v", MinSaltLen, err)
	}
}

func TestHashIsStable(t *testing.T) {
	h, err := New(testSalt, "v1")
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.Hash("developer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash("developer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("same input produced different pseudonyms:\n  %s\n  %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("pseudonym length = %d, want 64 hex characters", len(a))
	}
}

// Without normalization the same engineer acquires several pseudonyms and appears as several
// people in every cohort statistic — a silent correctness bug in the numbers the whole product
// reports.
func TestHashNormalizesIdentifiers(t *testing.T) {
	h, _ := New(testSalt, "v1")
	want, _ := h.Hash("developer@example.com")

	for _, variant := range []string{
		"Developer@Example.com",
		"DEVELOPER@EXAMPLE.COM",
		"  developer@example.com  ",
		"\tdeveloper@example.com\n",
	} {
		got, err := h.Hash(variant)
		if err != nil {
			t.Fatalf("Hash(%q): %v", variant, err)
		}
		if got != want {
			t.Errorf("Hash(%q) = %s, want %s — same person, different pseudonym", variant, got, want)
		}
	}
}

func TestHashSeparatesDifferentIdentifiers(t *testing.T) {
	h, _ := New(testSalt, "v1")
	a, _ := h.Hash("alice@example.com")
	b, _ := h.Hash("bob@example.com")
	if a == b {
		t.Fatal("different identifiers collided")
	}
}

// A different salt must produce different pseudonyms, or the salt is not doing its job and two
// organizations could compare hashes to identify shared staff.
func TestHashDependsOnSalt(t *testing.T) {
	h1, _ := New(testSalt, "v1")
	h2, _ := New(strings.Repeat("f", 40), "v1")
	a, _ := h1.Hash("developer@example.com")
	b, _ := h2.Hash("developer@example.com")
	if a == b {
		t.Fatal("pseudonym did not change with the salt")
	}
}

func TestHashRejectsEmptyActor(t *testing.T) {
	h, _ := New(testSalt, "v1")
	for _, in := range []string{"", "   ", "\t\n"} {
		if _, err := h.Hash(in); err != ErrEmptyActor {
			t.Errorf("Hash(%q) error = %v, want %v", in, err, ErrEmptyActor)
		}
	}
}

// Hashers reach log lines and error values by accident. The salt must not travel with them.
func TestStringDoesNotLeakSalt(t *testing.T) {
	h, _ := New(testSalt, "v7")
	s := h.String()
	if strings.Contains(s, testSalt) {
		t.Fatalf("String() leaked the salt: %s", s)
	}
	if !strings.Contains(s, "v7") {
		t.Errorf("String() should name the salt version, got: %s", s)
	}
}

func TestVersionIsReported(t *testing.T) {
	h, _ := New(testSalt, "2026-q3")
	if h.Version() != "2026-q3" {
		t.Fatalf("Version() = %q, want %q", h.Version(), "2026-q3")
	}
}
