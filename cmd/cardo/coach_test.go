package main

import (
	"testing"

	"github.com/amanachenko/cardo/internal/coach"
)

// The two windows are labelled separately: a window of imported history was never looked at, and
// the previous column must not read "prev 0" there. The usual start is an import followed by live
// telemetry, which is the case that used to claim "0 failed last time".
func TestToolsLineLabelsEachWindowOnItsOwn(t *testing.T) {
	live := coach.Window{ToolsObserved: true, ToolCalls: 5, FailedTools: 2}
	imported := coach.Window{ToolsObserved: false}

	for _, c := range []struct {
		name          string
		now, prev     coach.Window
		wantTools     string
		wantPrevTools string
	}{
		{"live over live", live, live, "5 (2 failed)", "prev 5"},
		{"live over imported", live, imported, "5 (2 failed)", "prev not observed"},
		{"imported over live", imported, live, "not observed", "prev 5"},
		{"imported over imported", imported, imported, "not observed", "prev not observed"},
	} {
		tools, prevTools := toolsLine(c.now, c.prev)
		if tools != c.wantTools {
			t.Errorf("%s: tools = %q, want %q", c.name, tools, c.wantTools)
		}
		if prevTools != c.wantPrevTools {
			t.Errorf("%s: previous = %q, want %q", c.name, prevTools, c.wantPrevTools)
		}
	}
}
