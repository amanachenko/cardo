package coach

import (
	"strings"
	"testing"
)

func TestSuggestNamesAConcreteFix(t *testing.T) {
	now := Window{ToolCalls: 20, FailedTools: 6, PeakContext: 200000, CostUSD: 9, Edits: 3}
	prev := Window{PeakContext: 100000, CostUSD: 3, Edits: 3}
	got := Suggest(now, prev, []string{"review-pr"})
	if len(got) != 3 {
		t.Fatalf("expected three suggestions, got %d: %v", len(got), got)
	}
	joined := strings.ToLower(strings.Join(got, " "))
	for _, want := range []string{"failed", "context"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	// Three evidence-based rules fill the budget; the peer hint waits its turn.
	if strings.Contains(joined, "review-pr") {
		t.Fatalf("peer hint should not displace evidence: %v", got)
	}
	peer := Suggest(Window{}, Window{}, []string{"review-pr"})
	if len(peer) != 1 || !strings.Contains(peer[0], "review-pr") {
		t.Fatalf("expected the peer hint alone, got %v", peer)
	}
	if strings.Contains(joined, "score") || strings.Contains(joined, "rank") {
		t.Fatalf("coach must not grade: %v", got)
	}
}

func TestSuggestStaysQuietWithoutEvidence(t *testing.T) {
	if got := Suggest(Window{}, Window{}, nil); len(got) != 0 {
		t.Fatalf("expected no suggestions, got %v", got)
	}
	now := Window{ToolCalls: 4, FailedTools: 1, PeakContext: 1000, CostUSD: 1, Edits: 5}
	prev := Window{PeakContext: 1000, CostUSD: 1, Edits: 5}
	if got := Suggest(now, prev, nil); len(got) != 0 {
		t.Fatalf("small samples must not produce advice: %v", got)
	}
}

func TestSuggestCapsAtThree(t *testing.T) {
	now := Window{ToolCalls: 50, FailedTools: 20, PeakContext: 400000, CostUSD: 20, Edits: 2}
	prev := Window{PeakContext: 100000, CostUSD: 2, Edits: 2}
	got := Suggest(now, prev, []string{"a", "b", "c", "d"})
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
}
