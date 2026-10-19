// Package directory turns a CSV of email,team into a hashed mapping. It never keeps an address past
// the call that hashed it (ADR-0046).
package directory

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amanachenko/cardo/internal/pseudonym"
)

// Entry is one person, before hashing.
type Entry struct {
	Email string
	Team  string
}

// Problem is one malformed line, with the line number a person can find in their file.
type Problem struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Preview is what the operator sees before anything is written: counts, never addresses.
type Preview struct {
	Rows       int            `json:"rows"`
	Unique     int            `json:"unique"`
	Duplicates int            `json:"duplicates"`
	Teams      int            `json:"teams"`
	TeamCounts map[string]int `json:"teamCounts"`
	Malformed  []Problem      `json:"malformed"`
}

// Parse reads a CSV with an optional header. A malformed line is reported, never skipped silently.
func Parse(text string) ([]Entry, Preview) {
	text = strings.TrimPrefix(text, "\ufeff")
	preview := Preview{TeamCounts: map[string]int{}}
	byEmail := map[string]Entry{}
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		preview.Rows++
		parts := strings.SplitN(line, ",", 2)
		email := strings.ToLower(strings.TrimSpace(parts[0]))
		if i == 0 && email == "email" {
			preview.Rows-- // header, not a person
			continue
		}
		if len(parts) < 2 {
			preview.Malformed = append(preview.Malformed, Problem{Line: i + 1, Reason: "expected email,team"})
			continue
		}
		team := strings.TrimSpace(parts[1])
		if !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
			preview.Malformed = append(preview.Malformed, Problem{Line: i + 1, Reason: "not an email address"})
			continue
		}
		if team == "" {
			preview.Malformed = append(preview.Malformed, Problem{Line: i + 1, Reason: "team is empty"})
			continue
		}
		if _, seen := byEmail[email]; seen {
			preview.Duplicates++
		}
		byEmail[email] = Entry{Email: email, Team: team}
	}

	entries := make([]Entry, 0, len(byEmail))
	for _, e := range byEmail {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Email < entries[j].Email })
	for _, e := range entries {
		preview.TeamCounts[e.Team]++
	}
	preview.Unique = len(entries)
	preview.Teams = len(preview.TeamCounts)
	if len(preview.Malformed) > 20 {
		preview.Malformed = preview.Malformed[:20]
	}
	return entries, preview
}

// Row is one hashed mapping, ready for storage. There is no email field on purpose.
type Row struct {
	Team      string
	Pseudonym string
}

// Hash turns entries into rows with the same hasher the collectors use, so the join is exact.
func Hash(entries []Entry, h *pseudonym.Hasher) ([]Row, error) {
	rows := make([]Row, 0, len(entries))
	for _, e := range entries {
		p, err := h.Hash(e.Email)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", e.Team, err)
		}
		rows = append(rows, Row{Team: e.Team, Pseudonym: p})
	}
	return rows, nil
}

// BelowFloor lists teams smaller than the floor; the console hides them and reports the count.
func BelowFloor(preview Preview, floor int) int {
	n := 0
	for _, count := range preview.TeamCounts {
		if count < floor {
			n++
		}
	}
	return n
}
