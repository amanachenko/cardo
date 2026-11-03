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
	})

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
	s := FromEnv(map[string]string{"CARDO_SALT": salt})
	page := s.Markdown()
	if strings.Contains(page, salt) {
		t.Error("the page prints the salt")
	}
	if !strings.Contains(page, "salt") {
		t.Error("the page does not mention the salt at all")
	}
}

// With no salt set, the page says so instead of naming a holder for a salt that does not exist:
// the collector refuses every batch until one is generated.
func TestThePageSaysWhenNoSaltIsSet(t *testing.T) {
	page := FromEnv(map[string]string{}).Markdown()
	if !strings.Contains(page, "No pseudonymisation salt is set yet") {
		t.Error("the page does not say that no salt is set")
	}
	if !strings.Contains(page, "refuses every batch") {
		t.Error("the page does not say what a missing salt means")
	}
}

// The holder is the operator's own words in CARDO_SALT_HOLDER. A hand-over to a security team is
// something the operator records, never something the page guesses from the team path (ADR-0034).
func TestThePageNamesTheHolderTheOperatorFilledIn(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_SALT":        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"CARDO_SALT_HOLDER": CustodySecurityTeam,
	})
	page := s.Markdown()
	if !strings.Contains(page, "held by "+CustodySecurityTeam) {
		t.Error("the page does not name the holder the operator filled in")
	}
}

// With the key empty the page names the operator who runs Cardo, and never claims a security team
// holds the salt — the launcher writes the salt into the operator's own .env, so no run of it can
// make that claim true.
func TestThePageNamesTheOperatorWhenTheHolderIsNotFilledIn(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_SALT": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	page := s.Markdown()
	if !strings.Contains(page, CustodyOperator) {
		t.Error("the page does not name the operator")
	}
	if strings.Contains(page, "security team") {
		t.Error("the page claims a security team holds the salt while the key is empty")
	}
}

// The Admin API is the one call that reaches outside the organization's network, and the page says
// so only when the key that causes it is configured.
func TestThePageMentionsTheAdminAPIOnlyWithAnAdminKey(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_SALT":      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"CARDO_ADMIN_KEY": "sk-admin-not-printed",
	})
	page := s.Markdown()
	if !strings.Contains(page, "Anthropic's Admin API") {
		t.Error("with an admin key set, the page does not say the poller reads usage reports")
	}
	if strings.Contains(page, "sk-admin-not-printed") {
		t.Error("the page prints the admin key")
	}
	if strings.Contains(FromEnv(map[string]string{}).Markdown(), "Anthropic's Admin API") {
		t.Error("without an admin key, the page still mentions the Admin API")
	}
}

// The page goes to engineers, so it must carry no address and no code name.
func TestThePageCarriesNoAddressAndNoPseudonym(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_SALT": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"EMAIL":      "someone@example.com",
	})
	page := s.Markdown()
	for _, bad := range []string{"someone@example.com", "@example.com", "someone"} {
		if strings.Contains(page, bad) {
			t.Errorf("the page carries %q", bad)
		}
	}
}

// A pattern can name the organization, and the page is handed around. It says that a pattern is
// set, not what the pattern is. The page links the public repository at the end, so the host name
// alone is not a leak; what must never appear is the pattern's own text, whole or in part.
func TestThePageDoesNotPrintThePatterns(t *testing.T) {
	s := FromEnv(map[string]string{
		"CARDO_ORG_ARTIFACTS": "^acme-secret-",
		"CARDO_ORG_REPOS":     "^github[.]com/acme-corp/",
	})
	page := s.Markdown()
	for _, bad := range []string{"^acme-secret-", "^github[.]com/acme-corp/", "acme-secret", "acme-corp"} {
		if strings.Contains(page, bad) {
			t.Errorf("the page prints the pattern %q", bad)
		}
	}
	if !strings.Contains(page, "a pattern naming them is set") {
		t.Error("the page does not say that a pattern is set")
	}
}

// The page goes to engineers, for whom "(ADR-0047)" means nothing. It carries no decision numbers
// and ends with one link to the privacy document.
func TestThePageCarriesNoDecisionNumbersAndLinksThePrivacyDocument(t *testing.T) {
	page := FromEnv(map[string]string{}).Markdown()
	for _, bad := range []string{"ADR-0", "INV-"} {
		if strings.Contains(page, bad) {
			t.Errorf("the page carries the decision number %q", bad)
		}
	}
	if !strings.Contains(page, "docs/design/privacy.md") {
		t.Error("the page does not link the privacy document")
	}
}

// The group size is the organization's own. The floor cannot be lowered, so a value below it is
// reported as the floor rather than as the organization's choice.
func TestTheGroupSizeNeverGoesBelowTheFloor(t *testing.T) {
	for _, in := range []string{"1", "3", "0", ""} {
		s := FromEnv(map[string]string{"CARDO_MIN_GROUP_SIZE": in})
		if got := s.minGroupSize(); got != DefaultMinGroupSize {
			t.Errorf("CARDO_MIN_GROUP_SIZE=%q gave a group size of %d, want %d", in, got, DefaultMinGroupSize)
		}
	}
	s := FromEnv(map[string]string{"CARDO_MIN_GROUP_SIZE": "10"})
	if got := s.minGroupSize(); got != 10 {
		t.Errorf("a raised group size gave %d, want 10", got)
	}
}

// The window is 90 days until the organization changes it, and the page says which it is.
func TestTheRetentionWindowIsTheDefaultUntilItIsChanged(t *testing.T) {
	s := FromEnv(map[string]string{})
	if !s.RetentionDefault || s.RetentionDays != DefaultRetentionDays {
		t.Errorf("with no setting the window is %d (default=%v), want %d (default=true)",
			s.RetentionDays, s.RetentionDefault, DefaultRetentionDays)
	}
	s = FromEnv(map[string]string{"CARDO_RETENTION_DAYS": "30"})
	if s.RetentionDefault {
		t.Error("a changed window is still reported as the default")
	}
	if s.RetentionDays != 30 {
		t.Errorf("the window is %d, want 30", s.RetentionDays)
	}
	// Zero is a choice, not a default: nothing expires on its own.
	s = FromEnv(map[string]string{"CARDO_RETENTION_DAYS": "0"})
	if s.RetentionDefault {
		t.Error("a window of zero is reported as the default")
	}
	if !strings.Contains(s.Markdown(), "nothing expires on its own") {
		t.Error("the page does not explain a window of zero")
	}
}

// A value Cardo does not document must not be reported as if it were in force.
func TestAnUndocumentedValueIsNotReportedAsInForce(t *testing.T) {
	s := FromEnv(map[string]string{"CARDO_ARTIFACT_NAMES": "everything"})
	if !strings.Contains(s.Markdown(), "not one Cardo documents") {
		t.Error("an undocumented naming mode is not flagged")
	}
}

// ADR-0035 says the notice states which way the organization set the instructions-names
// setting, so the page has to name the mode it is in — and say what the mode means, because the
// names are kept for every repository and shown only for the organization's own, once enough
// people load them.
func TestThePageStatesTheInstructionsNamesMode(t *testing.T) {
	s := FromEnv(map[string]string{"CARDO_INSTRUCTIONS_NAMES": "project"})
	page := s.Markdown()
	if !strings.Contains(page, "CARDO_INSTRUCTIONS_NAMES") {
		t.Errorf("the page does not name the setting\n---\n%s", page)
	}
	if !strings.Contains(page, "yes: kept for every repository, shown only for your organization's repositories once 5 people load them") {
		t.Errorf("the page does not say what keeping the names means\n---\n%s", page)
	}
	// The default: only the organization's own artifacts keep their names.
	s = FromEnv(map[string]string{})
	page = s.Markdown()
	if !strings.Contains(page, "no, only your organization's own artifacts") {
		t.Errorf("the page does not say that names are not kept by default\n---\n%s", page)
	}
	// A value Cardo does not document is the default, and the page says so.
	s = FromEnv(map[string]string{"CARDO_INSTRUCTIONS_NAMES": "projekt"})
	page = s.Markdown()
	if !strings.Contains(page, "not one Cardo documents") {
		t.Errorf("an undocumented instructions mode is not flagged\n---\n%s", page)
	}
	if strings.Contains(page, "yes: kept for every repository") {
		t.Errorf("an undocumented mode is reported as keeping names\n---\n%s", page)
	}
}
