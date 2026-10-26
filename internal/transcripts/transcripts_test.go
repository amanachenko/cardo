package transcripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A session is written to a fixture file, so the test reads what the importer reads.
func writeFixture(t *testing.T, name string, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const secretPrompt = "the quick brown fox jumps over the lazy dog"

func fixtureLines() []string {
	return []string{
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-10-01T10:00:00.000Z","uuid":"p1","message":{"role":"user","content":"` + secretPrompt + `"}}`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-10-01T10:00:05.000Z","uuid":"a1","parentUuid":"p1","message":{"role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}}}`,
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-10-01T10:01:00.000Z","uuid":"p2","message":{"role":"user","content":"second prompt"}}`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-10-01T10:01:03.000Z","uuid":"a2","parentUuid":"p2","message":{"role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":40,"output_tokens":8}}}`,
	}
}

// The importer keeps counts and lengths. It must not keep the text.
func TestScanKeepsCountsAndLengthsOnly(t *testing.T) {
	dir := writeFixture(t, "sess-1.jsonl", fixtureLines())
	st, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 1 {
		t.Errorf("files: got %d, want 1", st.Files)
	}
	if len(st.Sessions) != 1 {
		t.Fatalf("sessions: got %d, want 1", len(st.Sessions))
	}
	s := st.Sessions[0]
	if s.ID != "sess-1" {
		t.Errorf("session id: got %q, want %q", s.ID, "sess-1")
	}
	if s.Prompts != 2 {
		t.Errorf("prompts: got %d, want 2", s.Prompts)
	}
	if len(s.PromptLog) != 2 {
		t.Fatalf("prompt log: got %d entries, want 2", len(s.PromptLog))
	}
	if s.PromptLog[0].Len != len([]rune(secretPrompt)) {
		t.Errorf("prompt length: got %d, want %d", s.PromptLog[0].Len, len([]rune(secretPrompt)))
	}
	if s.PromptLog[0].UUID != "p1" {
		t.Errorf("prompt uuid: got %q, want %q", s.PromptLog[0].UUID, "p1")
	}
	if s.Usage.In != 140 || s.Usage.Out != 28 || s.Usage.CacheRead != 5 || s.Usage.CacheCreate != 7 {
		t.Errorf("usage: got in=%d out=%d cacheRead=%d cacheCreate=%d, want 140/28/5/7",
			s.Usage.In, s.Usage.Out, s.Usage.CacheRead, s.Usage.CacheCreate)
	}
	if len(s.Requests) != 2 {
		t.Fatalf("requests: got %d, want 2", len(s.Requests))
	}
	if s.Requests[0].PromptID != "p1" {
		t.Errorf("request prompt id: got %q, want %q", s.Requests[0].PromptID, "p1")
	}
	if s.Requests[0].Model != "claude-sonnet-4-5" {
		t.Errorf("request model: got %q, want %q", s.Requests[0].Model, "claude-sonnet-4-5")
	}
	if s.Start.IsZero() || s.End.IsZero() || s.End.Before(s.Start) {
		t.Errorf("span: start=%v end=%v", s.Start, s.End)
	}
}

// No field the importer returns may contain the prompt text. This is the invariant ADR-0050 rests
// on: the counts are kept, the words are not.
func TestScanNeverReturnsPromptText(t *testing.T) {
	dir := writeFixture(t, "sess-1.jsonl", fixtureLines())
	st, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Sessions {
		blob, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(blob), secretPrompt) {
			t.Fatalf("the session carries the prompt text: %s", blob)
		}
		if strings.Contains(string(blob), "second prompt") {
			t.Fatalf("the session carries the prompt text: %s", blob)
		}
	}
}

// A tool result is not a prompt, and a system reminder is not a prompt either.
func TestScanDoesNotCountToolResultsAsPrompts(t *testing.T) {
	dir := writeFixture(t, "sess-1.jsonl", []string{
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-10-01T10:00:00.000Z","uuid":"p1","message":{"role":"user","content":"do it"}}`,
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-10-01T10:00:01.000Z","uuid":"t1","isSidechain":true,"message":{"role":"user","content":[{"type":"tool_result","text":"the quick brown fox jumps over the lazy dog"}]}}`,
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-10-01T10:00:02.000Z","uuid":"r1","message":{"role":"user","content":"<system-reminder>noise</system-reminder>"}}`,
	})
	st, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Sessions) != 1 {
		t.Fatalf("sessions: got %d, want 1", len(st.Sessions))
	}
	if st.Sessions[0].Prompts != 1 {
		t.Errorf("prompts: got %d, want 1 (a tool result and a reminder are not prompts)", st.Sessions[0].Prompts)
	}
}
