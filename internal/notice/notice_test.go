package notice

import (
	"strings"
	"testing"
)

// The page must carry every field the roadmap asks for, so an engineer can read one page and know
// what the organization collects and who holds the key.
func TestThePageCarriesEveryRequiredField(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_SALT":           "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"CARDO_SALT_VERSION":   "v1",
		"CARDO_ORG_ARTIFACTS":  "^acme-",
		"CARDO_ORG_REPOS":      "^github[.]com/acme/",
		"CARDO_MIN_GROUP_SIZE": "5",
		"CARDO_RETENTION_DAYS": "90",
	}, CustodyOperator)

	page := s.Markdown()
	for _, want := range []string{
		"the operator",        // who holds the salt
		"CARDO_ORG_ARTIFACTS", // which settings are on
		"CARDO_MIN_GROUP_SIZE",
		"CARDO_RETENTION_DAYS",
		"not used for individual performance decisions",
		"v1", // the salt version in force
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not mention %q\n---\n%s", want, page)
		}
	}
}

// The salt is the key that could reveal who is behind a code name. The page names it as an
// artefact and never prints it.
func TestThePageNeverPrintsTheSalt(t *testing.T) {
	const salt = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	s := FromEnv(map[string]string{"CARDO_SALT": salt}, CustodyOperator)
	page := s.Markdown()
	if strings.Contains(page, salt) {
		t.Error("the page prints the salt")
	}
	if !strings.Contains(page, "salt") {
		t.Error("the page does not mention the salt at all")
	}
}

// The page goes to engineers, so it must carry no address and no code name.
func TestThePageCarriesNoAddressAndNoPseudonym(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_SALT": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"EMAIL":      "someone@example.com",
	}, CustodyOperator)
	page := s.Markdown()
	for _, bad := range []string{"someone@example.com", "@example.com", "someone"} {
		if strings.Contains(page, bad) {
			t.Errorf("the page carries %q", bad)
		}
	}
}

// A pattern can name the organization, and the page is handed around. It says that a pattern is
// set, not what the pattern is.
func TestThePageDoesNotPrintThePatterns(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_ORG_ARTIFACTS": "^acme-secret-",
		"CARDO_ORG_REPOS":     "^github[.]com/acme-corp/",
	}, CustodyOperator)
	page := s.Markdown()
	for _, bad := range []string{"acme-secret", "acme-corp", "github"} {
		if strings.Contains(page, bad) {
			t.Errorf("the page prints the pattern %q", bad)
		}
	}
	if !strings.Contains(page, "a pattern naming them is set") {
		t.Error("the page does not say that a pattern is set")
	}
}

// In a pilot the security team holds the salt, and the page must say so.
func TestThePageNamesTheSecurityTeamInAPilot(t *testing.T) {
	s := FromEnv(map[string]string{}, CustodySecurityTeam)
	page := s.Markdown()
	if !strings.Contains(page, "the security team") {
		t.Error("the page does not say that the security team holds the salt")
	}
}

// The group size is the organization's own. The floor cannot be lowered, so a value below it is
// reported as the floor rather than as the organization's choice.
func TestTheGroupSizeNeverGoesBelowTheFloor(t *testing.T) {
	for _, in := range []string{"1", "3", "0", ""} {
		s := FromEnv(map[string]string{"CARDO_MIN_GROUP_SIZE": in}, CustodyOperator)
		if got := s.minGroupSize(); got != DefaultMinGroupSize {
			t.Errorf("CARDO_MIN_GROUP_SIZE=%q gave a group size of %d, want %d", in, got, DefaultMinGroupSize)
		}
	}
	s := FromEnv(map[string]string{"CARDO_MIN_GROUP_SIZE": "10"}, CustodyOperator)
	if got := s.minGroupSize(); got != 10 {
		t.Errorf("a raised group size gave %d, want 10", got)
	}
}

// The window is 90 days until the organization changes it, and the page says which it is.
func TestTheRetentionWindowIsTheDefaultUntilItIsChanged(t *testing.T) {
	s := FromEnv(map[string]string{}, CustodyOperator)
	if !s.RetentionDefault || s.RetentionDays != DefaultRetentionDays {
		t.Errorf("with no setting the window is %d (default=%v), want %d (default=true)",
			s.RetentionDays, s.RetentionDefault, DefaultRetentionDays)
	}
	s = FromEnv(map[string]string{"CARDO_RETENTION_DAYS": "30"}, CustodyOperator)
	if s.RetentionDefault {
		t.Error("a changed window is still reported as the default")
	}
	if s.RetentionDays != 30 {
		t.Errorf("the window is %d, want 30", s.RetentionDays)
	}
	// Zero is a choice, not a default: nothing expires on its own.
	s = FromEnv(map[string]string{"CARDO_RETENTION_DAYS": "0"}, CustodyOperator)
	if s.RetentionDefault {
		t.Error("a window of zero is reported as the default")
	}
	if !strings.Contains(s.Markdown(), "nothing expires on its own") {
		t.Error("the page does not explain a window of zero")
	}
}

// A value Cardo does not document must not be reported as if it were in force.
func TestAnUndocumentedValueIsNotReportedAsInForce(t *testing.T) {
	s := FromEnv(map[string]string{"CARDO_ARTIFACT_NAMES": "everything"}, CustodyOperator)
	if !strings.Contains(s.Markdown(), "not one Cardo documents") {
		t.Error("an undocumented naming mode is not flagged")
	}
}
