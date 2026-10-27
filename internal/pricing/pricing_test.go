package pricing

import "testing"

// The published prices must not drift silently: a wrong rate writes a wrong cost into every row.
func TestPublishedPricesAreTheAnthropicList(t *testing.T) {
	tbl := Load()
	cases := []struct {
		model                                     string
		prompt, completion, cacheRead, cacheWrite float64
	}{
		// USD per token: the list price per million tokens, divided by a million.
		{"claude-opus-5", 5 / 1e6, 25 / 1e6, 0.50 / 1e6, 6.25 / 1e6},
		{"claude-opus-5-5", 4 / 1e6, 20 / 1e6, 0.20 / 1e6, 5 / 1e6},
		{"claude-sonnet-5", 2 / 1e6, 10 / 1e6, 0.20 / 1e6, 2.50 / 1e6},
		{"claude-sonnet-4-5", 3 / 1e6, 15 / 1e6, 0.30 / 1e6, 3.75 / 1e6},
		{"claude-haiku-4-5", 1 / 1e6, 5 / 1e6, 0.10 / 1e6, 1.25 / 1e6},
	}
	for _, c := range cases {
		p, ok := tbl.Resolve(c.model)
		if !ok {
			t.Errorf("%s: no price", c.model)
			continue
		}
		// The rates are divided by a million, so compare with a tolerance: the published numbers are
		// exact, the division is not.
		const eps = 1e-15
		if !close(p.Prompt, c.prompt, eps) || !close(p.Completion, c.completion, eps) ||
			!close(p.CacheRead, c.cacheRead, eps) || !close(p.CacheWrite, c.cacheWrite, eps) {
			t.Errorf("%s: got prompt=%g completion=%g cacheRead=%g cacheWrite=%g, want %g/%g/%g/%g",
				c.model, p.Prompt, p.Completion, p.CacheRead, p.CacheWrite,
				c.prompt, c.completion, c.cacheRead, c.cacheWrite)
		}
	}
}

func close(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

// Claude Code reports a dated id; it must resolve to the same price.
func TestResolveStripsTheDateSuffix(t *testing.T) {
	tbl := Load()
	dated, ok := tbl.Resolve("claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("the dated id has no price")
	}
	plain, ok := tbl.Resolve("claude-haiku-4-5")
	if !ok {
		t.Fatal("the plain id has no price")
	}
	if dated != plain {
		t.Errorf("the dated id costs %v, the plain one %v", dated, plain)
	}
}

// A model that is not in the table is unpriced. It is never guessed at.
func TestResolveRefusesAnUnknownModel(t *testing.T) {
	tbl := Load()
	for _, m := range []string{"", "<synthetic>", "gpt-4o", "claude-mystery-9"} {
		if _, ok := tbl.Resolve(m); ok {
			t.Errorf("%q: got a price, want none", m)
		}
	}
}

// The cost of one request is the sum of its parts.
func TestCostSumsTheParts(t *testing.T) {
	tbl := Load()
	p, ok := tbl.Resolve("claude-sonnet-5")
	if !ok {
		t.Fatal("no price")
	}
	got := p.Cost(1_000_000, 0, 0, 0)
	if got != 2 {
		t.Errorf("a million input tokens: got $%g, want $2", got)
	}
	got = p.Cost(0, 1_000_000, 0, 0)
	if got != 10 {
		t.Errorf("a million output tokens: got $%g, want $10", got)
	}
}
