package catalog

import (
	"strings"
	"testing"
)

const skillBody = `---
name: review-pr
description: Review a pull request against the house rules
---

# Review PR
`

func TestValidSkillPasses(t *testing.T) {
	problems := Validate(Upload{Kind: Skill, Slug: "review-pr", Name: "review-pr", Owner: "platform", FileName: "SKILL.md", Body: []byte(skillBody)}, 256<<10)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestFrontmatterIsRequired(t *testing.T) {
	body := "# Just a heading\n"
	problems := Validate(Upload{Kind: Skill, Slug: "x-skill", Name: "x-skill", Owner: "platform", FileName: "SKILL.md", Body: []byte(body)}, 0)
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, " "), "frontmatter") {
		t.Fatalf("problems = %v, want a frontmatter complaint", problems)
	}
}

func TestSecretIsRefused(t *testing.T) {
	body := skillBody + "\napi_key = sk-ant-abcdefghijklmnopqrstuvwx\n"
	problems := Validate(Upload{Kind: Skill, Slug: "leak", Name: "leak", Owner: "platform", FileName: "SKILL.md", Body: []byte(body)}, 0)
	joined := strings.Join(problems, " ")
	if !strings.Contains(joined, "secret") {
		t.Fatalf("problems = %v, want a secret refusal", problems)
	}
}

func TestSlugRules(t *testing.T) {
	for _, slug := range []string{"a", "-bad", "Has-Upper", "with space", "x"} {
		if ValidSlug(slug) {
			t.Errorf("%q passed, want refusal", slug)
		}
	}
	if !ValidSlug("review-pr-2") {
		t.Error("review-pr-2 should pass")
	}
}

func TestExtensionFollowsKind(t *testing.T) {
	body := `{"name":"figma","command":"npx"}`
	problems := Validate(Upload{Kind: MCPServer, Slug: "figma", Name: "figma.md", Owner: "platform", Body: []byte(body)}, 0)
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, " "), ".json") {
		t.Fatalf("problems = %v, want an extension complaint", problems)
	}
}

func TestReferenceNeedsWhenToUse(t *testing.T) {
	body := "# Deploy notes\n\nSome text.\n"
	problems := Validate(Upload{Kind: Reference, Slug: "deploy", Name: "deploy.md", Owner: "platform", Body: []byte(body)}, 0)
	if !strings.Contains(strings.Join(problems, " "), "When to use") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestDiffSummaryCountsLines(t *testing.T) {
	old := []byte("a\nb\nc\n")
	next := []byte("a\nB\nc\nd\n")
	got := DiffSummary(old, next)
	if !strings.Contains(got, "+2") || !strings.Contains(got, "−1") {
		t.Fatalf("summary = %q, want +2 lines and −1 line", got)
	}
	if DiffSummary(old, old) != "" {
		t.Error("identical bodies should have no diff")
	}
}
