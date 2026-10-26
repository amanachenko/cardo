package directory

import (
	"strings"
	"testing"

	"github.com/amanachenko/cardo/internal/pseudonym"
)

const sample = `email,team
Anna@Example.com,platform
boris@example.com,platform
chen@example.com,payments
broken-line
dana@example.com,
`

func TestParseCountsAndReportsProblems(t *testing.T) {
	entries, p := Parse(sample)
	if p.Rows != 5 || p.Unique != 3 {
		t.Fatalf("preview = %+v", p)
	}
	if len(p.Malformed) != 2 {
		t.Fatalf("malformed = %v, want two problems", p.Malformed)
	}
	if p.Malformed[0].Line != 5 {
		t.Errorf("first problem line = %d, want 5", p.Malformed[0].Line)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Email != "anna@example.com" {
		t.Errorf("email not normalised: %q", entries[0].Email)
	}
}

func TestDuplicatesLastWins(t *testing.T) {
	entries, p := Parse("email,team\na@x.com,one\na@x.com,two\n")
	if p.Duplicates != 1 || len(entries) != 1 || entries[0].Team != "two" {
		t.Fatalf("preview=%+v entries=%v", p, entries)
	}
}

func TestHashProducesPseudonymsNotAddresses(t *testing.T) {
	entries, _ := Parse("email,team\na@x.com,one\n")
	h, err := pseudonym.New(strings.Repeat("ab", 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Hash(entries, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Team != "one" {
		t.Fatalf("rows = %v", rows)
	}
	if strings.Contains(rows[0].Pseudonym, "@") || len(rows[0].Pseudonym) < 16 {
		t.Errorf("pseudonym = %q, want a hash", rows[0].Pseudonym)
	}
}

func TestBelowFloorCountsSmallTeams(t *testing.T) {
	_, p := Parse(sample)
	if n := BelowFloor(p, 5); n != 2 {
		t.Fatalf("below floor = %d, want 2", n)
	}
}
