package coach

import (
	"strings"
	"testing"
)

// The coach says one thing at a time, and the thing it says is what the fleet has shown
// (ADR-0041). A fixed tip about failed tool calls or growing context is a rule we wrote, not
// evidence the engineer's colleagues produced.
func TestSuggestNamesOnlyWhatTheFleetHasShown(t *testing.T) {
	got := Suggest([]PeerArtifact{{Name: "review-pr", People: 11}})
	if len(got) != 1 {
		t.Fatalf("expected one suggestion, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "11 people") {
		t.Errorf("suggestion = %q, want the count of people", got[0])
	}
	if !strings.Contains(got[0], "review-pr") {
		t.Errorf("suggestion = %q, want the artifact name", got[0])
	}
	joined := strings.ToLower(strings.Join(got, " "))
	for _, unwanted := range []string{"score", "rank", "percentile", "one in five", "compact"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("the coach must not grade or use a fixed tip: %v", got)
		}
	}
}

// With nothing the fleet has shown, the coach says nothing.
func TestSuggestStaysQuietWithoutEvidence(t *testing.T) {
	if got := Suggest(nil); len(got) != 0 {
		t.Fatalf("expected no suggestions, got %v", got)
	}
	if got := Suggest([]PeerArtifact{}); len(got) != 0 {
		t.Fatalf("expected no suggestions, got %v", got)
	}
}

// One suggestion at a time: the first is the one with the most people behind it.
func TestSuggestTakesOnlyTheFirst(t *testing.T) {
	got := Suggest([]PeerArtifact{
		{Name: "a", People: 12},
		{Name: "b", People: 9},
		{Name: "c", People: 5},
	})
	if len(got) != 1 {
		t.Fatalf("want one suggestion, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "a") {
		t.Errorf("suggestion = %q, want the most used artifact", got[0])
	}
}
