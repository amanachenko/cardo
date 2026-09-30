// Package test holds the invariant tests.
//
// These guard docs/design/invariants.md. They are deliberately blunt and deliberately noisy: each
// one protects a promise made to the engineers being measured, and each is easy to breach
// innocently while adding a useful feature.
//
// If one of these fails, it is the test doing its job. Fix the code, not the test. Changing an
// invariant requires a superseding ADR first.
package test

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/poll"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/store/jsonl"
)

const testSalt = "0123456789abcdef0123456789abcdef"

func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// goSources returns every non-test Go file in the repository.
func goSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "data") {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("found no Go sources to scan; the invariant tests would pass vacuously")
	}
	return out
}

// INV-1 — Session transcripts are never read. No code path opens anything under
// ~/.claude/projects/. Not for debugging, not once, not behind a flag.
//
// This is the single thing that would let a sceptic say "it reads my code", so the test scans for
// the path itself rather than for any particular way of opening it.
func TestINV1_NoTranscriptAccess(t *testing.T) {
	forbidden := []string{
		".claude/projects",
		`.claude\projects`,
		"claude/projects",
		"cleanupPeriodDays",
	}
	for file, body := range goSources(t) {
		for _, needle := range forbidden {
			if strings.Contains(body, needle) {
				t.Errorf("INV-1 violated: %s references %q.\n"+
					"  Session transcripts are never read — not for debugging, not once, not\n"+
					"  behind a flag. Changing this requires a superseding ADR (see ADR-0005).",
					file, needle)
			}
		}
	}
}

// INV-2 — user.email is never persisted. Pseudonymization happens before storage, always.
//
// Behavioural rather than static: the whole pipeline runs against a payload containing an email
// address, and every byte that reaches disk is searched for it. A static check could be satisfied
// by renaming a variable; this one cannot.
func TestINV2_NoEmailReachesDisk(t *testing.T) {
	const email = "veryspecific.person@example-corp.com"

	dir := t.TempDir()
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

	day, _ := time.Parse("2006-01-02", "2026-09-08")
	raw, _ := json.Marshal(map[string]any{
		"actor":        map[string]any{"type": "user_actor", "email_address": email},
		"core_metrics": map[string]any{"num_sessions": 4},
	})
	runner := &poll.Runner{
		Adapter: staticAdapter{records: []source.Record{{
			Source: source.Console, Day: day,
			ActorType: "user_actor", ActorID: email,
			ActorPath: []string{"actor", "email_address"},
			OrgID:     "org-1", Raw: raw,
		}}},
		Hasher: h, Store: st,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if _, err := runner.Run(context.Background(), source.Window{From: day, To: day}); err != nil {
		t.Fatal(err)
	}

	scanned := scanOutput(t, dir, email)
	if scanned == 0 {
		t.Fatal("nothing was written, so this test proved nothing")
	}
}

// scanOutput searches every file under dir for the identifier and for any 'email' key, and
// returns how many files it checked.
func scanOutput(t *testing.T, dir, email string) int {
	t.Helper()
	var scanned int
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		rel, _ := filepath.Rel(dir, path)
		if strings.Contains(strings.ToLower(string(b)), strings.ToLower(email)) {
			t.Errorf("INV-2 violated: %s contains a real email address.\n"+
				"  Pseudonymization must happen before storage, always.", rel)
		}
		if strings.Contains(string(b), "email") {
			t.Errorf("INV-2 violated: %s contains an 'email' key.\n"+
				"  No field that could hold one may reach storage.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return scanned
}

// INV-5 — No content in tiers 0 or 1: no prompt text, no code, no file paths, no tool parameters.
//
// Enforced as an allowlist over the bronze column set, so adding a column is a deliberate act
// that fails here first. That is the point: INV-4 requires an ADR before the mandatory set grows.
// bronzeColumns is the tier-1 column set. It is declared once and checked against every place
// bronze is written, so the JSONL store, the ClickHouse table and this list cannot drift apart.
var bronzeColumns = []string{
	"source", "day", "pseudonym", "salt_version", "tier", "actor_type",
	"org_id", "customer_type", "terminal_type", "payload", "unknown_fields",
}

func allowedBronzeColumns() map[string]bool {
	m := map[string]bool{}
	for _, c := range bronzeColumns {
		m[c] = true
	}
	return m
}

func TestINV5_BronzeColumnsAreAllowlisted(t *testing.T) {
	allowed := allowedBronzeColumns()

	dir := t.TempDir()
	st, _ := jsonl.New(dir)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h, _ := pseudonym.New(testSalt, "v1")

	day, _ := time.Parse("2006-01-02", "2026-09-08")
	raw := json.RawMessage(`{"actor":{"type":"user_actor","email_address":"a@b.com"},"core_metrics":{"num_sessions":1}}`)
	scrubbed, err := h.Scrub(source.Record{
		Source: source.Console, Day: day, ActorType: "user_actor",
		ActorID: "a@b.com", ActorPath: []string{"actor", "email_address"}, Raw: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(context.Background(), source.Console, "2026-09-08", []pseudonym.Scrubbed{scrubbed}); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "bronze", "source=console", "2026-09-08.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(string(b), "\n", 2)[0]), &row); err != nil {
		t.Fatal(err)
	}
	for col := range row {
		if !allowed[col] {
			t.Errorf("INV-5: bronze gained an unallowlisted column %q.\n"+
				"  Tiers 0 and 1 carry no content. If this column is genuinely needed, write the\n"+
				"  ADR first (INV-4), then add it to the allowlist here.", col)
		}
	}
}

// INV-5, second half — every OTEL_LOG_* flag in a shipped managed-settings bundle stays at 0.
//
// The bundle is a Phase 2 artifact and does not exist yet. This test activates on its own the
// moment it does, rather than waiting to be remembered.
func TestINV5_ShippedBundleLeavesLogFlagsOff(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "deploy", "managed-settings")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("deploy/managed-settings does not exist yet (Phase 2)")
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "OTEL_LOG_") {
				continue
			}
			if !strings.Contains(line, `"0"`) && !strings.Contains(line, ": 0") {
				rel, _ := filepath.Rel(repoRoot(t), path)
				t.Errorf("INV-5 violated: %s enables content logging:\n  %s",
					rel, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// INV-7 — No data leaves the organization's network. No phone-home, no benchmark upload, no hosted
// option. The only host the binary contacts is the Anthropic API it was pointed at.
func TestINV7_NoUnexpectedOutboundHosts(t *testing.T) {
	allowed := map[string]bool{
		"https://api.anthropic.com": true,
		// Module identity, never contacted at runtime.
		"https://github.com": true,
	}

	replacer := strings.NewReplacer(`"`, " ", "`", " ", ",", " ", ")", " ", "(", " ")
	for file, body := range goSources(t) {
		for _, tok := range strings.Fields(replacer.Replace(body)) {
			if !strings.HasPrefix(tok, "https://") && !strings.HasPrefix(tok, "http://") {
				continue
			}
			host := tok
			if i := strings.Index(tok[8:], "/"); i >= 0 {
				host = tok[:8+i]
			}
			if !allowed[host] {
				t.Errorf("INV-7: %s references an outbound host %q.\n"+
					"  No data leaves the organization's network. A documentation link belongs in a\n"+
					"  comment; a runtime call needs an ADR.", file, host)
			}
		}
	}
}

// staticAdapter replays a fixed record set for any day.
type staticAdapter struct{ records []source.Record }

func (s staticAdapter) Name() source.Name { return source.Console }

func (s staticAdapter) Fetch(ctx context.Context, day time.Time) ([]source.Record, error) {
	return s.records, nil
}

// INV-3 — No per-person view is visible to anyone except that person. Tier 1 is cohort-only.
// If a view would let a manager rank their reports, it is the wrong view.
//
// Gold is the layer dashboards read, so it is the layer where this is breached. A pseudonym may
// be counted — COUNT(DISTINCT pseudonym) is how cohort size is measured — but it may never be
// selected as an output column, because that is a leaderboard with one rename to go.
func TestINV3_GoldViewsExposeNoPerPersonRows(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "sql")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("no sql/ directory yet")
	}

	goldView := regexp.MustCompile(`view\s+(cardo\.)?gold_`)
	var checked, checkedClickHouse int
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot(t), path)

		var inGoldView bool
		for n, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			lower := strings.ToLower(trimmed)

			if strings.Contains(lower, "create or replace view") {
				// ClickHouse qualifies every view with the database, DuckDB does not. Matching
				// only the unqualified form once let every ClickHouse gold view go unchecked.
				inGoldView = goldView.MatchString(lower)
				if inGoldView {
					checked++
					if strings.Contains(path, "clickhouse") {
						checkedClickHouse++
					}
				}
				continue
			}
			if !inGoldView || strings.HasPrefix(trimmed, "--") {
				continue
			}
			if !strings.Contains(lower, "pseudonym") {
				continue
			}
			// Counting distinct actors is the legitimate use: it sizes a cohort without
			// naming anyone in it.
			if strings.Contains(lower, "count(distinct pseudonym)") {
				continue
			}
			t.Errorf("INV-3 violated: %s:%d exposes a pseudonym in a gold view:\n  %s\n"+
				"  Gold is cohort-only. A pseudonym may be counted, never selected — a per-person\n"+
				"  row here is a ranking a manager can sort.", rel, n+1, trimmed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no gold views were found to check; this test would pass vacuously")
	}
	// The collector-path gold views exist only in the ClickHouse dialect (ADR-0028), so a check
	// that silently skipped that dialect would guard none of them.
	if checkedClickHouse == 0 {
		t.Fatal("no ClickHouse gold views were found to check; the view pattern no longer matches them")
	}
}

// INV-5, third place: the ClickHouse bronze table.
//
// ADR-0009 requires both storage targets to work, which means both can leak. The column set is one
// list checked against three things -- this test, the JSONL row written to disk, and the DDL below
// -- so a column added to one store and not the other fails here rather than producing two
// installs that disagree about what Cardo collects.
func TestINV5_ClickHouseBronzeMatchesTheAllowlist(t *testing.T) {
	path := filepath.Join(repoRoot(t), "sql", "clickhouse", "001_bronze.sql")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no ClickHouse bronze migration yet: %v", err)
	}

	// Take the column block of the CREATE TABLE, stopping at the closing paren.
	body := string(b)
	i := strings.Index(body, "CREATE TABLE IF NOT EXISTS cardo.bronze_actor_day")
	if i < 0 {
		t.Fatal("could not find the bronze table definition; this test would pass vacuously")
	}
	open := strings.Index(body[i:], "(")
	end := strings.Index(body[i:], "\n)")
	if open < 0 || end < 0 || end < open {
		t.Fatal("could not delimit the bronze column list")
	}

	found := map[string]bool{}
	for _, line := range strings.Split(body[i+open+1:i+end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		col := strings.Fields(line)[0]
		found[strings.TrimSuffix(col, ",")] = true
	}

	allowed := allowedBronzeColumns()
	for col := range found {
		if !allowed[col] {
			t.Errorf("INV-5: ClickHouse bronze has an unallowlisted column %q.\n"+
				"  Tiers 0 and 1 carry no content. If this column is genuinely needed, write the\n"+
				"  ADR first (INV-4), then add it to bronzeColumns here.", col)
		}
	}
	for _, col := range bronzeColumns {
		if !found[col] {
			t.Errorf("ClickHouse bronze is missing the column %q.\n"+
				"  The two storage targets must hold the same columns, or the same query returns\n"+
				"  different answers depending on which one an organization deployed.", col)
		}
	}
}
