package coach

import (
	"encoding/json"
	"testing"
)

// The board iterates a report's suggestions, so the field must marshal as a list even when the
// window has none. A null here is the difference between an empty page and a crash.
func TestSuggestionsAlwaysMarshalAsAList(t *testing.T) {
	for _, untried := range [][]PeerArtifact{nil, {}} {
		b, err := json.Marshal(Suggest(untried))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "[]" {
			t.Fatalf("Suggest(%v) marshaled as %s, want []", untried, b)
		}
	}
}
