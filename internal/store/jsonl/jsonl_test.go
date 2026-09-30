package jsonl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
)

func scrubbed(t *testing.T, email string, sessions int) pseudonym.Scrubbed {
	t.Helper()
	h, err := pseudonym.New("0123456789abcdef0123456789abcdef", "v1")
	if err != nil {
		t.Fatal(err)
	}
	day, _ := time.Parse("2006-01-02", "2026-09-08")
	raw := json.RawMessage(`{"actor":{"type":"user_actor","email_address":"` + email +
		`"},"core_metrics":{"num_sessions":` + string(rune('0'+sessions)) + `}}`)
	s, err := h.Scrub(source.Record{
		Source: source.Console, Day: day, ActorType: "user_actor",
		ActorID: email, ActorPath: []string{"actor", "email_address"},
		OrgID: "org-1", CustomerType: "api", TerminalType: "vscode", Raw: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func TestPutWritesHivePartitionedFiles(t *testing.T) {
	st, dir := newStore(t)
	recs := []pseudonym.Scrubbed{scrubbed(t, "a@example.com", 3), scrubbed(t, "b@example.com", 1)}

	if err := st.Put(context.Background(), source.Console, "2026-09-08", recs); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "bronze", "source=console", "2026-09-08.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected a Hive-partitioned file DuckDB can read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2 (one JSON object per line)", len(lines))
	}
	for i, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if row["tier"].(float64) != 1 {
			t.Errorf("tier = %v, want 1 — Admin API aggregates are fleet-core data (ADR-0002)", row["tier"])
		}
		if row["salt_version"] != "v1" {
			t.Errorf("salt_version = %v, want v1 — stored so rotation stays possible", row["salt_version"])
		}
		if row["source"] != "console" {
			t.Errorf("source = %v, want console — the discriminator ADR-0021 requires", row["source"])
		}
	}
}

// Both analytics APIs revise recent days, so the poller re-fetches them. Appending would
// accumulate duplicate, diverging copies of the same day and quietly double every total.
func TestPutReplacesRatherThanAppends(t *testing.T) {
	st, dir := newStore(t)
	ctx := context.Background()

	if err := st.Put(ctx, source.Console, "2026-09-08", []pseudonym.Scrubbed{
		scrubbed(t, "a@example.com", 3), scrubbed(t, "b@example.com", 1),
	}); err != nil {
		t.Fatal(err)
	}
	// The same day polled again, now revised down to one actor.
	if err := st.Put(ctx, source.Console, "2026-09-08", []pseudonym.Scrubbed{
		scrubbed(t, "a@example.com", 5),
	}); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(filepath.Join(dir, "bronze", "source=console", "2026-09-08.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 {
		t.Fatalf("re-polling a day left %d rows, want 1 — the day must be replaced, not appended", len(lines))
	}
}

// An interrupted write must not leave a half-written file, because DuckDB would refuse to parse
// the whole partition and the operator would see a broken store rather than a stale one.
func TestPutLeavesNoPartialFiles(t *testing.T) {
	st, dir := newStore(t)
	if err := st.Put(context.Background(), source.Console, "2026-09-08",
		[]pseudonym.Scrubbed{scrubbed(t, "a@example.com", 3)}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "bronze", "source=console"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".") {
			t.Errorf("temporary file %q survived the write", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("partition holds %d files, want exactly 1", len(entries))
	}
}

func TestPutHandlesEmptyDay(t *testing.T) {
	st, dir := newStore(t)
	if err := st.Put(context.Background(), source.Console, "2026-09-08", nil); err != nil {
		t.Fatal(err)
	}
	// The file must exist and be empty: an absent file is indistinguishable from a day that was
	// never polled, which would make a resumed backfill re-fetch it forever.
	path := filepath.Join(dir, "bronze", "source=console", "2026-09-08.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("an empty day must still leave a marker file: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("empty day wrote %d bytes, want 0", info.Size())
	}
}

func TestNewRejectsEmptyDirectory(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected an error for an empty output directory")
	}
}
