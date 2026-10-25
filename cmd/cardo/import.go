package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"context"

	"github.com/amanachenko/cardo/internal/pricing"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/transcripts"
)

// runImport previews (and later writes) a one-time import of local Claude Code transcripts.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	useSynopsis(fs, "import")
	dir := fs.String("dir", filepath.Join(homeDir(), ".claude", "projects"), "transcript root")
	email := fs.String("email", gitEmail(), "your address; hashed, never stored")
	write := fs.Bool("write", false, "write rows into ClickHouse (default is a preview)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" || !strings.Contains(*email, "@") {
		return fmt.Errorf("need an email for the pseudonym: -email you@example.com")
	}
	table := pricing.Load()
	stats, err := transcripts.Scan(*dir)
	if err != nil {
		return err
	}
	var in, out, cache, prompts int64
	var cost float64
	unknownModels := map[string]bool{}
	var first, last time.Time
	models := map[string]int{}
	for _, s := range stats.Sessions {
		in += s.Usage.In
		out += s.Usage.Out
		cache += s.Usage.CacheRead
		prompts += int64(s.Prompts)
		for model, u := range s.ModelsTok {
			pr, ok := table.Resolve(model)
			if !ok {
				unknownModels[model] = true
				continue
			}
			cost += pr.Cost(u.In, u.Out, u.CacheRead, u.CacheCreate)
		}
		if !s.Start.IsZero() && (first.IsZero() || s.Start.Before(first)) {
			first = s.Start
		}
		if s.End.After(last) {
			last = s.End
		}
		for m := range s.Models {
			models[m]++
		}
	}
	fmt.Printf("TRANSCRIPTS  %d files · %d sessions · %d prompts\n", stats.Files, len(stats.Sessions), prompts)
	fmt.Printf("TOKENS       in %d · out %d · cache read %d\n", in, out, cache)
	fmt.Printf("COST         $%.2f at list price · %d models unpriced\n", cost, len(unknownModels))
	if len(unknownModels) > 0 {
		names := make([]string, 0, len(unknownModels))
		for m := range unknownModels {
			names = append(names, m)
		}
		sort.Strings(names)
		fmt.Printf("UNPRICED     %s\n", strings.Join(names, " · "))
	}
	if !first.IsZero() {
		fmt.Printf("SPAN         %s → %s\n", first.Format("2006-01-02"), last.Format("2006-01-02"))
	}
	top := make([]string, 0, len(models))
	for m, n := range models {
		top = append(top, fmt.Sprintf("%s×%d", m, n))
	}
	sort.Strings(top)
	fmt.Printf("MODELS       %s\n", strings.Join(top, " · "))
	fmt.Printf("EMAIL        %s\n", *email)
	fmt.Printf("             this must be the address Claude Code signs in with, or the imported\n")
	fmt.Printf("             history will not join the sessions collected live\n")
	fmt.Printf("PSEUDONYM    %s\n", mustPseudonym(*email))
	if !*write {
		fmt.Println("PREVIEW ONLY — rerun with -write to store these sessions.")
		return nil
	}
	salt := os.Getenv("CARDO_SALT")
	if salt == "" {
		return fmt.Errorf("CARDO_SALT is required to write (it is in deploy/compose/.env)")
	}
	version := saltVersion()
	id, err := pseudonym.ForEmail(salt, version, *email)
	if err != nil {
		return err
	}
	return writeSessions(context.Background(), table, os.Stdout, id.Value, "imported", version, stats.Sessions)
}

// saltVersion names the pseudonymisation salt generation. It matches what the collector writes, so
// imported history joins the sessions collected live instead of sitting beside them.
func saltVersion() string {
	if v := strings.TrimSpace(os.Getenv("CARDO_SALT_VERSION")); v != "" {
		return v
	}
	return "v1"
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func gitEmail() string {
	out, err := exec.Command("git", "config", "--global", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func mustPseudonym(email string) string {
	salt := os.Getenv("CARDO_SALT")
	if salt == "" {
		return "(no CARDO_SALT in env)"
	}
	id, err := pseudonym.ForEmail(salt, saltVersion(), email)
	if err != nil {
		return "(invalid email)"
	}
	return id.Value[:16] + "…"
}

func chPost(ctx context.Context, sql string) error {
	url := os.Getenv("CARDO_CLICKHOUSE_URL")
	if url == "" {
		url = "http" + "://" + "127.0.0.1:8124"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(sql))
	if err != nil {
		return err
	}
	req.Header.Set("X-ClickHouse-User", envOr("CARDO_CLICKHOUSE_USER", "default"))
	req.Header.Set("X-ClickHouse-Key", os.Getenv("CARDO_CLICKHOUSE_PASSWORD"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clickhouse: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func esc(s string) string {
	return strings.NewReplacer("\\", "\\\\", "'", "\\'", "\n", " ", "\r", " ").Replace(s)
}

// chQuery runs a read-only query and returns its body.
func chQuery(ctx context.Context, sql string) (string, error) {
	url := os.Getenv("CARDO_CLICKHOUSE_URL")
	if url == "" {
		url = "http" + "://" + "127.0.0.1:8124"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(sql))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-ClickHouse-User", envOr("CARDO_CLICKHOUSE_USER", "default"))
	req.Header.Set("X-ClickHouse-Key", os.Getenv("CARDO_CLICKHOUSE_PASSWORD"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("clickhouse: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

func ts(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return "toDateTime64('" + t.UTC().Format("2006-01-02 15:04:05.000000000") + "', 9)"
}

func writeSessions(ctx context.Context, table pricing.Table, out io.Writer, pseudonym, cohort, saltVersion string, sessions []*transcripts.Session) error {
	if len(sessions) == 0 {
		return nil
	}
	// A session that already arrived through live telemetry is counted once, by the collector. The
	// importer replaces only its own earlier rows, so without this the same session would be counted
	// twice: cost, tokens and prompts.
	live, err := liveSessions(ctx, sessions)
	if err != nil {
		return err
	}
	if len(live) > 0 {
		fmt.Fprintf(out, "SKIPPED      %d sessions already collected live\n", len(live))
		kept := make([]*transcripts.Session, 0, len(sessions))
		for _, s := range sessions {
			if !live[s.ID] {
				kept = append(kept, s)
			}
		}
		sessions = kept
		if len(sessions) == 0 {
			fmt.Fprintf(out, "WROTE        0 rows for 0 sessions\n")
			return nil
		}
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, "'"+esc(s.ID)+"'")
	}
	if err := chPost(ctx, "ALTER TABLE cardo.bronze_otel_logs DELETE WHERE LogAttributes['cardo.import'] = '1' AND LogAttributes['session.id'] IN ("+strings.Join(ids, ",")+") SETTINGS mutations_sync = 1"); err != nil {
		return err
	}
	rows := 0
	var values []string
	flush := func() error {
		if len(values) == 0 {
			return nil
		}
		sql := "INSERT INTO cardo.bronze_otel_logs (Timestamp, TraceId, SpanId, TraceFlags, SeverityText, SeverityNumber, ServiceName, Body, ResourceSchemaUrl, ResourceAttributes, ScopeSchemaUrl, ScopeName, ScopeVersion, ScopeAttributes, LogAttributes, EventName) VALUES " + strings.Join(values, ",")
		if err := chPost(ctx, sql); err != nil {
			return err
		}
		rows += len(values)
		values = nil
		return nil
	}
	res := func() string {
		return "map('user.pseudonym','" + esc(pseudonym) + "','cardo.cohort','" + esc(cohort) + "','cardo.salt_version','" + esc(saltVersion) + "','service.name','claude-code')"
	}
	for _, s := range sessions {
		for _, p := range s.PromptLog {
			logAttrs := "map('session.id','" + esc(s.ID) + "','prompt.id','" + esc(p.UUID) + "','prompt_length','" + fmt.Sprint(p.Len) + "','user.pseudonym','" + esc(pseudonym) + "','cardo.import','1')"
			values = append(values, "("+ts(p.TS)+",'','',0,'',0,'','','',"+res()+",'','','',map(),"+logAttrs+",'claude_code.user_prompt')")
		}
		for _, r := range s.Requests {
			if r.PromptID == "" {
				continue
			}
			ctxTokens := r.Usage.In + r.Usage.CacheRead + r.Usage.CacheCreate
			// An unpriced model writes no cost at all. Writing 0 would read as "this cost nothing"
			// in the views; the preview's UNPRICED line is where the gap is stated.
			costAttr := ""
			if price, ok := table.Resolve(r.Model); ok {
				cost := price.Cost(r.Usage.In, r.Usage.Out, r.Usage.CacheRead, r.Usage.CacheCreate)
				costAttr = ",'cost_usd','" + fmt.Sprintf("%.10f", cost) + "'"
			}
			logAttrs := fmt.Sprintf("map('session.id','%s','prompt.id','%s','model','%s','query_source','repl_main_thread','input_tokens','%d','output_tokens','%d','cache_read_tokens','%d','cache_creation_tokens','%d','context_tokens','%d'%s,'duration_ms','0','user.pseudonym','%s','cardo.import','1')",
				esc(s.ID), esc(r.PromptID), esc(r.Model), r.Usage.In, r.Usage.Out, r.Usage.CacheRead, r.Usage.CacheCreate, ctxTokens, costAttr, esc(pseudonym))
			values = append(values, "("+ts(r.TS)+",'','',0,'',0,'','','',"+res()+",'','','',map(),"+logAttrs+",'claude_code.api_request')")
		}
		if len(values) >= 200 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "WROTE        %d rows for %d sessions\n", rows, len(sessions))
	return nil
}

// liveSessions finds the imported sessions that already have rows from live telemetry: a row in
// bronze_otel_logs or bronze_hook_events that the collector wrote, without the import marker.
func liveSessions(ctx context.Context, sessions []*transcripts.Session) (map[string]bool, error) {
	live := map[string]bool{}
	if len(sessions) == 0 {
		return live, nil
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, "'"+esc(s.ID)+"'")
	}
	in := strings.Join(ids, ",")
	sql := "SELECT DISTINCT session_id FROM (" +
		"SELECT LogAttributes['session.id'] AS session_id FROM cardo.bronze_otel_logs " +
		"WHERE LogAttributes['session.id'] IN (" + in + ") AND (LogAttributes['cardo.import'] = '' OR LogAttributes['cardo.import'] != '1') " +
		"UNION ALL " +
		"SELECT LogAttributes['session_id'] AS session_id FROM cardo.bronze_hook_events " +
		"WHERE LogAttributes['session_id'] IN (" + in + ")" +
		") FORMAT TSV"
	body, err := chQuery(ctx, sql)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			live[id] = true
		}
	}
	return live, nil
}
