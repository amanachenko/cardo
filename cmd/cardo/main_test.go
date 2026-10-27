package main

import (
	"strings"
	"testing"
	"time"
)

func TestResolveWindow(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, from, to string
		days           int
		want           [2]string // first and last day; empty when an error is expected
	}{
		{"default ends yesterday", "", "", 30, [2]string{"2026-09-08", "2026-10-07"}},
		{"days ends yesterday", "", "", 7, [2]string{"2026-10-01", "2026-10-07"}},
		{"days before -to", "", "2026-01-10", 7, [2]string{"2026-01-04", "2026-01-10"}},
		{"one day at -to", "", "2026-01-10", 1, [2]string{"2026-01-10", "2026-01-10"}},
		{"from and to", "2026-01-01", "2026-01-10", 30, [2]string{"2026-01-01", "2026-01-10"}},
		{"from to yesterday", "2026-09-30", "", 30, [2]string{"2026-09-30", "2026-10-07"}},
		{"from after to", "2026-01-11", "2026-01-10", 30, [2]string{}},
		{"no days", "", "", 0, [2]string{}},
		{"no days before -to", "", "2026-01-10", 0, [2]string{}},
		{"unparsable to", "", "10/01/2026", 7, [2]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := resolveWindow(now, tc.from, tc.to, tc.days)
			if tc.want[0] == "" {
				if err == nil {
					t.Fatalf("got %s to %s, want an error", w.From.Format("2006-01-02"), w.To.Format("2006-01-02"))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := [2]string{w.From.Format("2006-01-02"), w.To.Format("2006-01-02")}
			if got != tc.want {
				t.Errorf("window = %s to %s, want %s to %s", got[0], got[1], tc.want[0], tc.want[1])
			}
			if n := len(w.Days()); n == 0 {
				t.Errorf("the window holds no days, so a poll over it does nothing")
			}
		})
	}
}

// Every command the usage lists must answer `cardo help <command>`: a listed command without a
// synopsis prints "no help" instead.
func TestEveryListedCommandHasHelp(t *testing.T) {
	listed := usage[:strings.Index(usage, "Examples")]
	for _, line := range strings.Split(listed, "\n") {
		f := strings.Fields(line)
		if !strings.HasPrefix(line, "  ") || len(f) < 2 || f[0] == "cardo" || f[0] == "help" {
			continue
		}
		if _, ok := synopsis[f[0]]; !ok {
			t.Errorf("%q is listed in the usage but has no synopsis", f[0])
		}
	}
}
