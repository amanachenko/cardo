// Package notice writes the page an organization hands to its engineers.
//
// The page says three things the roadmap asks for: who holds the salt, which settings are on, and
// that the data is not for individual performance decisions. It is written from the settings file,
// so it cannot claim a setting the organization has not actually turned on.
//
// Two rules shape the output. It carries no email address, no pseudonym and no secret — the salt
// is named as an artefact that exists, never printed, because the salt is the key that could reveal
// who is behind a code name. And it says what the organization chose, in the words of the settings
// file, so an engineer can check it against the file their machine has.
package notice

import (
	"fmt"
	"strings"
)

// Settings are the values the notice reads. They come from the stack's .env, so the page describes
// the installation that exists rather than a default.
type Settings struct {
	// SaltVersion labels the salt, so an engineer can ask which one is in force.
	SaltVersion string
	// SaltPresent says the salt exists. The salt itself is never in this struct.
	SaltPresent bool
	// Custody is who holds the salt: the operator, or a security team.
	Custody string
	// OrgArtifacts is the pattern naming the organization's own artifacts, empty when none is set.
	OrgArtifacts string
	// ArtifactNames is the naming mode: "all" or "org-only".
	ArtifactNames string
	// OrgRepos is the pattern naming the organization's repositories.
	OrgRepos string
	// MinGroupSize is how many people a name needs before a view shows it.
	MinGroupSize int
	// RetentionDays is the window raw rows are kept. Zero means nothing expires on its own.
	RetentionDays int
	// RetentionDefault says the window is the built-in one and nobody changed it.
	RetentionDefault bool
}

// DefaultMinGroupSize is the floor when the organization has not chosen one. It matches the
// greatest() in the settings view, so the page and the store cannot disagree (ADR-0029).
const DefaultMinGroupSize = 5

// DefaultRetentionDays is the window when the organization has not chosen one (ADR-0053).
const DefaultRetentionDays = 90

// CustodyOperator and CustodySecurityTeam are the two holders the page names.
const (
	CustodyOperator     = "the operator"
	CustodySecurityTeam = "the security team"
)

// Markdown renders the notice.
//
// The order is the order a reader needs: what this is, who holds the key, what is collected, what
// is not, and what the data is not used for.
func (s Settings) Markdown() string {
	var b strings.Builder

	b.WriteString("# What this organization collects about your Claude Code sessions\n\n")
	b.WriteString("Cardo measures the tools your teams built, not the people who use them. This page\n")
	b.WriteString("says what is collected, who holds the key to it, and what it is not used for. It is\n")
	b.WriteString("written from the settings file on the machine that runs Cardo, so it describes the\n")
	b.WriteString("installation you actually have.\n\n")

	b.WriteString("## Who holds the salt\n\n")
	fmt.Fprintf(&b, "The pseudonymisation salt is held by %s.\n", s.custody())
	fmt.Fprintf(&b, "It is the key that could reveal who is behind a code name, so it is not sent\n")
	fmt.Fprintf(&b, "anywhere and is not printed here. The salt version in force is %s.\n\n", s.saltVersion())

	b.WriteString("## What is collected\n\n")
	b.WriteString("Sessions are counted. The counts carry a code name, never your address, and your\n")
	b.WriteString("address is not stored at all — it is turned into the code name before anything is\n")
	b.WriteString("written (ADR-0006).\n\n")
	b.WriteString("The names of the tools your organization ships are kept, so a rollout can be seen to\n")
	b.WriteString("spread. A name one person uses is never shown by name: it is counted as \"other\"\n")
	b.WriteString("until enough people use it (ADR-0029).\n\n")

	b.WriteString("| Setting | In force |\n")
	b.WriteString("| --- | --- |\n")
	fmt.Fprintf(&b, "| `CARDO_ORG_ARTIFACTS` — your organization's own artifacts | %s |\n", s.orgArtifacts())
	fmt.Fprintf(&b, "| `CARDO_ARTIFACT_NAMES` — names of tools engineers define themselves | %s |\n", s.artifactNames())
	fmt.Fprintf(&b, "| `CARDO_ORG_REPOS` — your organization's repositories | %s |\n", s.orgRepos())
	fmt.Fprintf(&b, "| Names of project instruction files | %s |\n", s.instructionsNames())
	fmt.Fprintf(&b, "| `CARDO_MIN_GROUP_SIZE` — people needed before a name is shown | %d |\n", s.minGroupSize())
	fmt.Fprintf(&b, "| `CARDO_RETENTION_DAYS` — raw rows kept | %s |\n\n", s.retention())

	b.WriteString("## What is never collected\n\n")
	b.WriteString("Your prompts, your assistant's answers, your code, your file paths and your tool\n")
	b.WriteString("arguments are never collected. The telemetry flags that would carry them are off, and\n")
	b.WriteString("the collector drops them if a client sends them anyway (ADR-0025, ADR-0039).\n\n")
	b.WriteString("Nothing leaves your organization's network. Cardo contacts no host at all: it reads\n")
	b.WriteString("the Claude Code telemetry your machines send to a collector you run (ADR-0047).\n\n")

	b.WriteString("## What the data is not used for\n\n")
	b.WriteString("Cardo's data is not used for individual performance decisions, and there is no view\n")
	b.WriteString("that would let a manager rank their reports. A person's own figures are seen by that\n")
	b.WriteString("person and by nobody else, through a private link (ADR-0032).\n\n")

	b.WriteString("## If something looks wrong\n\n")
	b.WriteString("The operator can show you the settings file and the check that proves what arrived.\n")
	b.WriteString("Ask for `cardo check` on the machine that runs Cardo.\n")

	return b.String()
}

func (s Settings) custody() string {
	if c := strings.TrimSpace(s.Custody); c != "" {
		return c
	}
	return CustodyOperator
}

func (s Settings) saltVersion() string {
	if v := strings.TrimSpace(s.SaltVersion); v != "" {
		return v
	}
	return "v1"
}

func (s Settings) orgArtifacts() string {
	if p := strings.TrimSpace(s.OrgArtifacts); p != "" {
		return "yes, a pattern naming them is set"
	}
	return "no, none are named"
}

func (s Settings) artifactNames() string {
	switch m := strings.TrimSpace(s.ArtifactNames); m {
	case "", "all":
		return "yes, every one that is not a generic name"
	case "org-only":
		return "no, only the organization's own"
	default:
		return fmt.Sprintf("yes, but in the mode %q, which is not one Cardo documents", m)
	}
}

func (s Settings) orgRepos() string {
	if p := strings.TrimSpace(s.OrgRepos); p != "" {
		return "yes, a pattern naming them is set"
	}
	return "no, every repository is external"
}

// instructionsNames says what the collector does with an instructions file's name.
//
// It does not report a setting, because the setting ADR-0035 names is not implemented yet, and a
// page that claimed a setting was in force when it was not would be worse than no page. It reports
// the rule the collector actually applies: a name is kept when it matches the organization's
// artifact pattern, and dropped otherwise (ADR-0026). When the setting lands, this line changes
// with it.
func (s Settings) instructionsNames() string {
	if p := strings.TrimSpace(s.OrgArtifacts); p != "" {
		return "yes, the ones matching your organization's artifact pattern"
	}
	return "no, only whether a file was loaded"
}

func (s Settings) minGroupSize() int {
	if s.MinGroupSize < DefaultMinGroupSize {
		return DefaultMinGroupSize
	}
	return s.MinGroupSize
}

func (s Settings) retention() string {
	switch {
	case s.RetentionDefault:
		return fmt.Sprintf("%d days (the default)", DefaultRetentionDays)
	case s.RetentionDays == 0:
		return "everything, and nothing expires on its own"
	case s.RetentionDays < 0:
		return fmt.Sprintf("%d days, which is not a value Cardo documents", s.RetentionDays)
	default:
		return fmt.Sprintf("%d days", s.RetentionDays)
	}
}

// FromEnv reads the notice's settings out of a parsed .env.
//
// The values come from the file and not from the environment, because the file is what the
// organization chose and what an engineer can be shown. A missing key means the setting is off,
// which is what an absent setting means everywhere else in Cardo.
func FromEnv(env map[string]string, custody string) Settings {
	s := Settings{
		SaltVersion:      strings.TrimSpace(env["CARDO_SALT_VERSION"]),
		SaltPresent:      strings.TrimSpace(env["CARDO_SALT"]) != "",
		Custody:          custody,
		OrgArtifacts:     strings.TrimSpace(env["CARDO_ORG_ARTIFACTS"]),
		ArtifactNames:    strings.TrimSpace(env["CARDO_ARTIFACT_NAMES"]),
		OrgRepos:         strings.TrimSpace(env["CARDO_ORG_REPOS"]),
		MinGroupSize:     DefaultMinGroupSize,
		RetentionDays:    DefaultRetentionDays,
		RetentionDefault: true,
	}
	if n := strings.TrimSpace(env["CARDO_MIN_GROUP_SIZE"]); n != "" {
		if v, ok := parseInt(n); ok && v >= DefaultMinGroupSize {
			s.MinGroupSize = v
		}
	}
	if n := strings.TrimSpace(env["CARDO_RETENTION_DAYS"]); n != "" {
		if v, ok := parseInt(n); ok && v >= 0 {
			s.RetentionDays = v
			s.RetentionDefault = v == DefaultRetentionDays
		}
	}
	return s
}

// parseInt reads a decimal number without pulling in strconv's error text into the page.
func parseInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
