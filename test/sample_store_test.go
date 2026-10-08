package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/store/jsonl"
)

// TestGenerateSampleStore writes a sample store for verifying the SQL layer against data the real
// store actually produced, rather than against JSONL written by hand to match it.
//
// Skipped unless CARDO_SAMPLE_DIR is set, so ordinary test runs leave no artifacts. A relative
// value is resolved against the repository root:
//
//	CARDO_SAMPLE_DIR=.sample go test ./test/ -run TestGenerateSampleStore
//
// Then read it with `make sql`, which drives the DuckDB CLI on stdin. Stdin rather than
// `duckdb -c` is not a style choice: `.read` is an input-loop command, and `-c` parses its
// argument as SQL, so the `-c` form fails with a syntax error on the leading dot.
func TestGenerateSampleStore(t *testing.T) {
	dir := os.Getenv("CARDO_SAMPLE_DIR")
	if dir == "" {
		t.Skip("set CARDO_SAMPLE_DIR to generate a sample store")
	}
	// A relative path is resolved against the repository root, not against this package's
	// directory. `go test` runs each package with its own directory as the working directory, so
	// the obvious CARDO_SAMPLE_DIR=.sample would otherwise write into test/ -- somewhere no
	// documented duckdb command looks, and somewhere an unanchored .gitignore rule has to cover.
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot(t), dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	t.Logf("sample store: %s", dir)

	st, err := jsonl.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h, err := pseudonym.New(testSalt, "v1")
	if err != nil {
		t.Fatal(err)
	}

	actors := []struct {
		email    string
		terminal string
		customer string
	}{
		{"alice@example.com", "vscode", "api"},
		{"bob@example.com", "iTerm.app", "api"},
		{"carol@example.com", "tmux", "subscription"},
	}

	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for d := 0; d < 5; d++ {
		day := start.AddDate(0, 0, d)
		key := day.Format("2006-01-02")

		var rows []pseudonym.Scrubbed
		for i, a := range actors {
			// A little variation so aggregates are not uniformly identical, and one actor with
			// a high rejection rate so the acceptance mart has something to show.
			accepted := 10 + i*5 + d
			rejected := 1 + i*4
			raw := fmt.Sprintf(`{
  "date": %q,
  "actor": {"type": "user_actor", "email_address": %q},
  "organization_id": "dc9f6c26-b22c-4831-8d01-0446bada88f1",
  "customer_type": %q,
  "terminal_type": %q,
  "core_metrics": {
    "num_sessions": %d,
    "lines_of_code": {"added": %d, "removed": %d},
    "commits_by_claude_code": %d,
    "pull_requests_by_claude_code": %d
  },
  "tool_actions": {
    "edit_tool": {"accepted": %d, "rejected": %d},
    "multi_edit_tool": {"accepted": %d, "rejected": 0},
    "write_tool": {"accepted": %d, "rejected": %d},
    "notebook_edit_tool": {"accepted": 0, "rejected": 0}
  },
  "model_breakdown": [
    {"model": "claude-opus-5", "tokens": {"input": %d, "output": %d, "cache_read": 1000, "cache_creation": 500},
     "estimated_cost": {"currency": "USD", "amount": %d}},
    {"model": "claude-sonnet-5", "tokens": {"input": %d, "output": %d, "cache_read": 200, "cache_creation": 100},
     "estimated_cost": {"currency": "USD", "amount": %d}}
  ]
}`,
				day.Format(time.RFC3339), a.email, a.customer, a.terminal,
				2+i+d, 100*(i+1)+d*10, 20*(i+1), i, d%2,
				accepted, rejected, 2+i, 3+i, i,
				10000*(i+1), 3000*(i+1), 113+i*40,
				5000*(i+1), 1200*(i+1), 22+i*7)

			// One actor-day without two of the tools and one token count, as a source that leaves
			// out a zero would send it. test/duckdb_checks.sql requires each to read as 0, not NULL.
			if d == 2 && i == 1 {
				for _, field := range []string{
					`"multi_edit_tool": {"accepted": 3, "rejected": 0},`,
					",\n    \"notebook_edit_tool\": {\"accepted\": 0, \"rejected\": 0}",
					`, "cache_creation": 500`,
				} {
					if !strings.Contains(raw, field) {
						t.Fatalf("the sample template changed, and %q is no longer there to leave out", field)
					}
					raw = strings.Replace(raw, field, "", 1)
				}
			}

			rec := source.Record{
				Source: source.Console, Day: day,
				ActorType: "user_actor", ActorID: a.email,
				ActorPath:    []string{"actor", "email_address"},
				OrgID:        "dc9f6c26-b22c-4831-8d01-0446bada88f1",
				CustomerType: a.customer, TerminalType: a.terminal,
				Raw: json.RawMessage(raw),
			}
			// One day carries drift, so gold_schema_drift has something to report.
			if d == 3 && i == 0 {
				rec.Unknown = []string{"core_metrics.skills_invoked"}
			}
			s, err := h.Scrub(rec)
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, s)
		}

		if err := st.Put(context.Background(), source.Console, key, rows); err != nil {
			t.Fatal(err)
		}
	}

	abs, _ := filepath.Abs(dir)
	t.Logf("sample store written to %s", abs)
}
