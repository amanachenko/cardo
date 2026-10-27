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
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/poll"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/store/clickhouse"
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

// INV-1 — The collection path never reads session transcripts. No automatic code path opens anything
// under ~/.claude/projects/. Not for debugging, not once, not behind a flag.
//
// This is the single thing that would let a sceptic say "it reads my code", so the test scans for
// the path itself rather than for any particular way of opening it — including a path assembled from
// parts, which is how the first import slipped past this test.
//
// The one sanctioned exception is the local, user-invoked importer (ADR-0050): it reads metadata only
// and never runs on the collection path.
func TestINV1_NoTranscriptAccess(t *testing.T) {
	forbidden := []string{
		".claude/projects",
		`.claude\projects`,
		"claude/projects",
		"cleanupPeriodDays",
	}
	// Files allowed to assemble the transcript root, and why. Keep this list short: adding a file is
	// a design decision, not a formality (ADR-0050).
	localImport := map[string]bool{
		"cmd/cardo/import.go": true,
		// The installer's visible `history` step invokes the same command for the person running it.
		"internal/launcher/step_history.go": true,
	}

	seen := map[string]bool{}
	sources := goSources(t)
	for file, body := range sources {
		for _, needle := range forbidden {
			if strings.Contains(body, needle) && !localImport[file] {
				t.Errorf("INV-1 violated: %s references %q.\n"+
					"  The collection path never reads session transcripts. The local importer is the\n"+
					"  only sanctioned reader and is named in ADR-0050.",
					file, needle)
			}
		}
		// A path assembled from parts defeats the literals above; catch that form too.
		if strings.Contains(body, `".claude"`) && strings.Contains(body, "projects") {
			if !localImport[file] {
				t.Errorf("INV-1 violated: %s assembles the transcript directory from parts.\n"+
					"  Naming the components separately does not make the read allowed (ADR-0050).", file)
			}
			seen[file] = true
		}
	}
	for file := range localImport {
		if _, ok := sources[file]; !ok {
			t.Errorf("INV-1 allowlist names %s, which no longer exists; prune it", file)
		}
	}
}

// INV-2 — user.email is never persisted. Pseudonymization happens before storage, always.
//
// Behavioural rather than static: the whole pipeline runs against a payload containing an email
// address, and every byte sent to the store is searched for it. A static check could be satisfied
// by renaming a variable; this one cannot. TestINV2_NoEmailReachesClickHouse searches a real
// server's rows the same way, when one is running.
func TestINV2_NoEmailReachesTheStore(t *testing.T) {
	const email = "veryspecific.person@example-corp.com"

	st, sent := recordingStore(t)
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

	if len(sent.inserts()) == 0 {
		t.Fatal("nothing was inserted, so this test proved nothing")
	}
	scanSent(t, sent, email)
}

// INV-2, the poller's tripwire. An email address in a field the adapter does not model, someone
// other than the actor, stops the poll before that day is stored. The collector's tripwire stops
// on the same pattern.
func TestINV2_PollerStopsOnAnEmailItDidNotModel(t *testing.T) {
	const actor, other = "veryspecific.person@example-corp.com", "a.reviewer@example-corp.com"

	st, sent := recordingStore(t)
	h, err := pseudonym.New(testSalt, "v1")
	if err != nil {
		t.Fatal(err)
	}

	day, _ := time.Parse("2006-01-02", "2026-09-08")
	raw, _ := json.Marshal(map[string]any{
		"actor":        map[string]any{"type": "user_actor", "email_address": actor},
		"core_metrics": map[string]any{"num_sessions": 4},
		"review":       map[string]any{"reviewer": other},
	})
	runner := &poll.Runner{
		Adapter: staticAdapter{records: []source.Record{{
			Source: source.Console, Day: day,
			ActorType: "user_actor", ActorID: actor,
			ActorPath: []string{"actor", "email_address"},
			OrgID:     "org-1", Raw: raw,
		}}},
		Hasher: h, Store: st,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err = runner.Run(context.Background(), source.Window{From: day, To: day})
	scanSent(t, sent, other)
	scanSent(t, sent, actor)
	var leak pseudonym.ErrIdentityLeak
	if !errors.As(err, &leak) {
		t.Fatalf("Run() error = %v, want ErrIdentityLeak.\n"+
			"  INV-2 violated: the poll went on past an email address the adapter does not model.", err)
	}
}

// sentRequests is what a recording server was sent: each request's query string and body.
type sentRequests struct {
	mu   sync.Mutex
	reqs []sentRequest
}

type sentRequest struct{ query, body string }

// inserts returns the bodies of the INSERT requests, the rows a real server would store.
func (s *sentRequests) inserts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.reqs {
		if strings.Contains(r.query, "INSERT") {
			out = append(out, r.body)
		}
	}
	return out
}

// recordingStore is the ClickHouse store pointed at a server on loopback that answers every
// request with an empty success and keeps what it was sent. Put runs exactly as it would against
// a real ClickHouse; nothing is stored anywhere.
func recordingStore(t *testing.T) (*clickhouse.Store, *sentRequests) {
	t.Helper()
	sent := &sentRequests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent.mu.Lock()
		sent.reqs = append(sent.reqs, sentRequest{query: r.URL.RawQuery, body: string(b)})
		sent.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	st, err := clickhouse.New(clickhouse.Config{Endpoint: srv.URL},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, sent
}

// scanSent searches every request the store sent for the identifier and for any 'email' key.
func scanSent(t *testing.T, sent *sentRequests, email string) {
	t.Helper()
	sent.mu.Lock()
	defer sent.mu.Unlock()
	for i, r := range sent.reqs {
		b := r.query + "\n" + r.body
		if strings.Contains(strings.ToLower(b), strings.ToLower(email)) {
			t.Errorf("INV-2 violated: request %d to the store contains a real email address.\n"+
				"  Pseudonymization must happen before storage, always.", i+1)
		}
		if strings.Contains(b, "email") {
			t.Errorf("INV-2 violated: request %d to the store contains an 'email' key.\n"+
				"  No field that could hold one may reach storage.", i+1)
		}
	}
}

// INV-5 — No content in tiers 0 or 1: no prompt text, no code, no file paths, no tool parameters.
//
// Enforced as an allowlist over the bronze column set, so adding a column is a deliberate act
// that fails here first. That is the point: INV-4 requires an ADR before the mandatory set grows.
// bronzeColumns is the tier-1 column set. It is declared once and checked against both places
// bronze is defined, the row the store sends and the ClickHouse table, so neither can drift from it.
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

	st, sent := recordingStore(t)
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

	rows := sent.inserts()
	if len(rows) != 1 {
		t.Fatalf("the store sent %d inserts, want 1", len(rows))
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(rows[0], "\n", 2)[0]), &row); err != nil {
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
		// The made-up stack's collector, on loopback: cmd/cardo-madeup's only destination, and never
		// another machine (ADR-0051). test/madeup_test.go holds both to the ports that stack publishes.
		"http://127.0.0.1:8090": true,
		"http://127.0.0.1:4320": true,
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
// be counted, which is how a group is sized without naming anyone in it, but it may never be
// selected as an output column, because that is a leaderboard with one rename to go.
//
// Each gold view is judged by its last definition, in the order the runner applies the
// migrations, because that is the one a dashboard reads. Every line of it is read, the CREATE
// line too. 012 wrote its views on one line each, as ClickHouse prints them, and while this check
// skipped the CREATE line it read nothing of them: a view on one line that selected every
// pseudonym passed.
func TestINV3_GoldViewsExposeNoPerPersonRows(t *testing.T) {
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "sql", "clickhouse", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)

	type sqlLine struct {
		n    int
		text string
	}
	type definition struct {
		file  string
		lines []sqlLine
	}
	created := regexp.MustCompile(`^\s*create\s+(?:or\s+replace\s+)?(?:materialized\s+)?view\s+` +
		`(?:if\s+not\s+exists\s+)?(?:cardo\.)?(gold_\w+)`)
	dropped := regexp.MustCompile(`^\s*drop\s+(?:view|table)\s+(?:if\s+exists\s+)?(?:cardo\.)?(gold_\w+)`)
	final := map[string]definition{}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, path)
		var stmt []sqlLine
		for n, text := range strings.Split(string(b), "\n") {
			// As the runner splits a file: a comment runs to the end of its line, and a semicolon
			// ends a statement.
			if i := strings.Index(text, "--"); i >= 0 {
				text = text[:i]
			}
			parts := strings.Split(text, ";")
			for i, part := range parts {
				if strings.TrimSpace(part) != "" {
					stmt = append(stmt, sqlLine{n + 1, part})
				}
				if i == len(parts)-1 {
					break
				}
				var whole strings.Builder
				for _, l := range stmt {
					whole.WriteString(strings.ToLower(l.text) + "\n")
				}
				if m := created.FindStringSubmatch(whole.String()); m != nil {
					final[m[1]] = definition{rel, stmt}
				} else if m := dropped.FindStringSubmatch(whole.String()); m != nil {
					delete(final, m[1])
				}
				stmt = nil
			}
		}
	}
	if len(final) == 0 {
		t.Fatal("no gold views were found to check; this test would pass vacuously")
	}

	// The ways a view counts people. countDistinctIf with this one condition leaves out a row
	// without a pseudonym (ADR-0036), as gold_fleet_adoption does; any other condition could
	// count one person.
	counted := regexp.MustCompile(`count\(\s*distinct\s+(?:\w+\.)?pseudonym\s*\)` +
		`|countdistinct\(\s*(?:\w+\.)?pseudonym\s*\)` +
		`|countdistinctif\(\s*(?:\w+\.)?pseudonym\s*,\s*(?:\w+\.)?pseudonym\s*!=\s*''\s*\)`)
	views := make([]string, 0, len(final))
	for v := range final {
		views = append(views, v)
	}
	sort.Strings(views)
	for _, v := range views {
		def := final[v]
		for _, l := range def.lines {
			rest := counted.ReplaceAllString(strings.ToLower(l.text), "")
			i := strings.Index(rest, "pseudonym")
			if i < 0 {
				continue
			}
			// A view on one line is hundreds of characters; show the part around the pseudonym.
			excerpt := strings.TrimSpace(rest[max(0, i-60):min(len(rest), i+70)])
			t.Errorf("INV-3 violated: %s:%d exposes a pseudonym in %s, as last defined:\n  ...%s...\n"+
				"  Gold is cohort-only. A pseudonym may be counted, never selected — a per-person\n"+
				"  row here is a ranking a manager can sort.", def.file, l.n, v, excerpt)
		}
	}
}

// INV-5, second place: the ClickHouse bronze table.
//
// The column set is one list checked against the row the store sends (above) and the DDL below,
// so a column added to the table and not the row, or the other way round, fails here rather than
// at the first insert.
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
				"  The store sends it on every row, and an insert with a column the table lacks\n"+
				"  is refused.", col)
		}
	}
}

// Service runs are labelled and never counted as people (ADR-0036). The views must keep the label
// out of people counts, and the migration must carry the label from the resource attributes.
func TestServiceRunsStayOutOfPeopleCounts(t *testing.T) {
	body, err := os.ReadFile("../sql/clickhouse/012_service_runs.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, want := range []string{
		"cardo.actor",
		"cardo.workflow",
		"actor_kind",
		"countDistinctIf(pseudonym, pseudonym != '')",
		"actor_kind != 'service'",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("012_service_runs.sql does not contain %q", want)
		}
	}
	if n := strings.Count(sql, "actor_kind != 'service'"); n < 2 {
		t.Errorf("gold views filter service sessions %d times, want at least 2", n)
	}

	// 012 is not the last word. A later migration that redefines a view whole can drop the filter,
	// as the enablement review's first draft of gold_artifact_usage did. So from 012 on, every gold
	// view that counts people in the collector path's silver leaves service runs out, by itself or
	// by reading silver_session_cohort_week, and that view holds none.
	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "sql", "clickhouse", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	create := regexp.MustCompile(`(?i)create\s+or\s+replace\s+view\s+cardo\.(\w+)`)
	checked := 0
	for _, path := range paths {
		name := filepath.Base(path)
		if name < "012" {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range strings.Split(withoutLineComments(string(b)), ";") {
			m := create.FindStringSubmatch(stmt)
			if m == nil {
				continue
			}
			view, filtered := m[1], strings.Contains(stmt, "actor_kind != 'service'")
			switch {
			case view == "silver_session_cohort_week":
				checked++
				if !filtered {
					t.Errorf("%s: silver_session_cohort_week puts service runs in teams, so every weekly "+
						"view that reads it counts them as people (ADR-0036)", name)
				}
			case strings.HasPrefix(view, "gold_") && strings.Contains(stmt, "pseudonym") && readsCollectorSilver(stmt):
				checked++
				if !filtered && !strings.Contains(stmt, "cardo.silver_session_cohort_week") {
					t.Errorf("%s: %s counts people with service runs among them (ADR-0036). Leave them "+
						"out with actor_kind != 'service', as 012 does, or read silver_session_cohort_week", name, view)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no view from 012 on was checked; this test would pass vacuously")
	}
}

// withoutLineComments drops each line's `--` comment, as the migration runner does, so a comment
// that mentions a filter does not count as the filter.
func withoutLineComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// readsCollectorSilver reports whether a statement reads one of the collector path's silver views,
// where service runs are labelled. The Admin API's silver has no such label.
func readsCollectorSilver(stmt string) bool {
	for _, v := range []string{"silver_session", "silver_api_request", "silver_turn", "silver_tool_call",
		"silver_policy_decision", "silver_context_event", "silver_artifact_load"} {
		if strings.Contains(stmt, "cardo."+v) {
			return true
		}
	}
	return false
}
