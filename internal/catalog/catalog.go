// Package catalog holds the parts of the enablement catalog that do not touch storage: the kind
// allow-list, slug rules, per-kind structure checks, the secret scan, and the summary a person sees
// before a version is saved. Keeping it free of I/O is what makes those rules testable without a
// stack (ADR-0045).
package catalog

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Kind is one of the artifact types the console can hold.
type Kind string

const (
	Skill     Kind = "skill"
	Command   Kind = "command"
	Subagent  Kind = "subagent"
	MCPServer Kind = "mcp"
	Reference Kind = "reference"
	Bundle    Kind = "bundle"
	Cohort    Kind = "cohort"
)

// Kinds is the allow-list, in the order the console shows it.
var Kinds = []Kind{Skill, Command, Subagent, MCPServer, Reference, Bundle, Cohort}

// Valid reports whether k is one of the known kinds.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Ext is the extension a kind's file must carry.
func (k Kind) Ext() string {
	switch k {
	case MCPServer, Bundle, Cohort:
		return ".json"
	default:
		return ".md"
	}
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

// ValidSlug reports whether a slug is safe to use as a directory name and readable as a name.
func ValidSlug(slug string) bool { return slugRE.MatchString(slug) }

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)api[_-]?key\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)secret\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// ScanSecrets returns the patterns that matched. A catalog is shared with engineers; a key pasted
// into a skill would be distributed by design.
func ScanSecrets(body []byte) []string {
	text := string(body)
	var found []string
	for _, re := range secretPatterns {
		if re.MatchString(text) {
			found = append(found, re.String())
		}
	}
	return found
}

// Upload is what the operator submitted, before anything is written.
type Upload struct {
	Kind  Kind
	Slug  string
	Name  string
	Owner string
	// FileName is what the browser sent. It is the only trustworthy source for the extension: a name
	// field without a dot used to be silently given the kind's extension.
	FileName string
	Body     []byte
}

// Validate returns every problem it can name at once. An empty result means the upload may be saved.
func Validate(u Upload, maxBytes int) []string {
	var problems []string
	if !u.Kind.Valid() {
		problems = append(problems, fmt.Sprintf("kind %q is not one of %s", u.Kind, joinKinds()))
	}
	if !ValidSlug(u.Slug) {
		problems = append(problems, "slug must be 2–64 characters of a-z, 0-9 and -, and start with a letter or digit")
	}
	if strings.TrimSpace(u.Name) == "" {
		problems = append(problems, "name is required")
	}
	if strings.TrimSpace(u.Owner) == "" {
		problems = append(problems, "owner is required")
	}
	if len(u.Body) == 0 {
		problems = append(problems, "the file is empty")
	}
	if maxBytes > 0 && len(u.Body) > maxBytes {
		problems = append(problems, fmt.Sprintf("the file is %d bytes; the ceiling is %d", len(u.Body), maxBytes))
	}
	if ext := path.Ext(u.slugFile()); ext != u.Kind.Ext() {
		problems = append(problems, fmt.Sprintf("a %s is %s; this file is %q", u.Kind, u.Kind.Ext(), ext))
	}
	if len(problems) > 0 {
		return problems
	}
	switch u.Kind {
	case Skill, Command, Subagent:
		problems = append(problems, requireFrontmatter(string(u.Body))...)
	case MCPServer:
		problems = append(problems, requireJSONKeys(string(u.Body), "name", "command")...)
	case Bundle:
		problems = append(problems, requireJSONKeys(string(u.Body), "env", "hooks")...)
	case Reference:
		problems = append(problems, requireHeadings(string(u.Body))...)
	case Cohort:
		problems = append(problems, requireJSONKeys(string(u.Body), "teams")...)
	}
	if found := ScanSecrets(u.Body); len(found) > 0 {
		sort.Strings(found)
		problems = append(problems, "the file looks like it contains a secret ("+strings.Join(found, ", ")+"); a catalog is handed to engineers, so a key would be distributed")
	}
	return problems
}

// slugFile is the uploaded file's own name, used only to check its extension against the kind.
func (u Upload) slugFile() string {
	if u.FileName != "" {
		return u.FileName
	}
	return u.Name
}

func joinKinds() string {
	out := make([]string, len(Kinds))
	for i, k := range Kinds {
		out[i] = string(k)
	}
	return strings.Join(out, ", ")
}

func requireFrontmatter(body string) []string {
	if !strings.HasPrefix(strings.TrimSpace(body), "---") {
		return []string{"the file must start with YAML frontmatter delimited by ---"}
	}
	rest := strings.TrimPrefix(strings.TrimSpace(body), "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return []string{"the frontmatter is not closed with ---"}
	}
	head := rest[:end]
	var problems []string
	for _, key := range []string{"name:", "description:"} {
		if !strings.Contains(head, key) {
			problems = append(problems, "the frontmatter is missing "+strings.TrimSuffix(key, ":"))
		}
	}
	return problems
}

func requireHeadings(body string) []string {
	var problems []string
	if !strings.HasPrefix(strings.TrimSpace(body), "# ") {
		problems = append(problems, "a reference document starts with a level-1 heading")
	}
	if !strings.Contains(strings.ToLower(body), "## when to use") {
		problems = append(problems, `a reference document needs a "## When to use" section`)
	}
	return problems
}

func requireJSONKeys(body string, keys ...string) []string {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return []string{"the file is not a JSON object: " + err.Error()}
	}
	var problems []string
	for _, key := range keys {
		if _, ok := doc[key]; !ok {
			problems = append(problems, fmt.Sprintf("the JSON is missing %q", key))
		}
	}
	return problems
}

// DiffSummary is what the operator reads before saving: a count and the first changed lines, not a
// full patch. An empty summary means the body is identical to the approved version.
func DiffSummary(oldBody, newBody []byte) string {
	if string(oldBody) == string(newBody) {
		return ""
	}
	oldLines := strings.Split(strings.TrimRight(string(oldBody), "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(string(newBody), "\n"), "\n")

	oldSet := map[string]int{}
	for _, l := range oldLines {
		oldSet[l]++
	}
	added, removed := 0, 0
	var samples []string
	for _, l := range newLines {
		if oldSet[l] > 0 {
			oldSet[l]--
			continue
		}
		added++
		if len(samples) < 4 && strings.TrimSpace(l) != "" {
			samples = append(samples, "+"+truncate(l))
		}
	}
	for l, n := range oldSet {
		if n <= 0 {
			continue
		}
		removed += n
		if len(samples) < 6 && strings.TrimSpace(l) != "" {
			samples = append(samples, "-"+truncate(l))
		}
	}
	if added == 0 && removed == 0 {
		return "the text is reordered; no lines were added or removed"
	}
	out := fmt.Sprintf("+%d −%d lines", added, removed)
	if len(samples) > 0 {
		out += ": " + strings.Join(samples, " · ")
	}
	return out
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}
