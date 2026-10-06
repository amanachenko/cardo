package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/store/clickhouse"
)

// Collector contract tests.
//
// deploy/collector/config.yaml is the privacy contract for the whole Phase 2 path, and it is
// configuration rather than code, so nothing else checks it. Two kinds of test hold it to account:
//
//   - Static tests parse the config and always run. They pin the hook allowlist, and they check
//     that every pipeline fails closed and that nothing exports anywhere but ClickHouse.
//   - Live tests POST fixtures to a running collector and read back what actually reached
//     ClickHouse. They skip unless the reference stack is up:
//
//	make stack-up && make collector-test
//
// The live tests need the same CARDO_SALT the collector was started with, because the point of
// one of them is to check that the collector's pseudonym is byte-for-byte the poller's.

// hookAllowlist is every hook field that may reach storage. It is the collector's keep_keys list,
// pinned here so that adding to it is a two-place change that a reviewer sees.
//
// Adding a field here is adding to the mandatory tier. INV-4 says that needs an ADR first.
var hookAllowlist = []string{
	"hook_event_name", "session_id", "prompt_id", "agent_id", "agent_type",
	"permission_mode", "effort_level",
	"session_start_reason", "model", "session_end_reason",
	"prompt_length",
	"command_name", "command_source", "expansion_type",
	"tool_name", "tool_use_id",
	"compaction_reason", "current_context_tokens", "target_tokens", "tokens_before", "tokens_after",
	"load_reason", "memory_type", "content_hash", "instructions_file", "instructions_scope",
	"instructions_name", // ADR-0026
	"from_model", "to_model",
	"requested_model", "model_switch_source", "context_tokens", "prompt_cache_warm", "cache_ttl",
	"estimated_cache_write_usd", // ADR-0027
	"config_source",
}

// hookContentFields may never be on the allowlist. Most carry content, paths or model output. The
// rest are generic names whose meaning depends on the event (reason, trigger, source, message,
// error): the allowlist is a union across events, so one of these is mapped onto a specific name
// for one event instead. This is a second lock: the pinned list above already excludes them, but
// a change that edits both the config and that list would still have to get past this one.
var hookContentFields = []string{
	"user_input", "prompt", "expanded_prompt", "command_args", "tool_input", "tool_response",
	"last_assistant_message", "cwd", "transcript_path", "scratchpad_dir", "agent_transcript_path",
	"file_path", "parent_file_path", "trigger_file_path", "globs", "permission_suggestions", "background_tasks",
	"session_crons", "custom_instructions", "compact_summary",
	"message", "reason", "error", "source", "trigger",
}

// contentMarkers appear in every content-bearing value of the fixtures. None may be stored.
var contentMarkers = []string{"CONTENT-MARKER", "marker-user", "example-corp"}

func collectorConfig(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "collector", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// sectionBlocks splits a top-level YAML section into its direct children, keyed by name, each
// holding the child's own lines. The config is ours and regular, so indentation is enough; a YAML
// library would be the repository's first dependency (ADR-0022).
func sectionBlocks(cfg, section string) map[string]string {
	out := map[string]string{}
	in, cur := false, ""
	for _, line := range strings.Split(cfg, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			in, cur = trimmed == section+":", ""
		case in && indent == 2:
			cur = strings.TrimSuffix(trimmed, ":")
			out[cur] = ""
		case in && cur != "":
			out[cur] += line + "\n"
		}
	}
	return out
}

// pipelines returns service.pipelines as name -> field -> list.
func pipelines(cfg string) map[string]map[string][]string {
	out := map[string]map[string][]string{}
	inService, inPipelines, cur := false, false, ""
	for _, line := range strings.Split(cfg, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			inService, inPipelines = trimmed == "service:", false
		case inService && indent == 2:
			inPipelines = trimmed == "pipelines:"
		case inPipelines && indent == 4:
			cur = strings.TrimSuffix(trimmed, ":")
			out[cur] = map[string][]string{}
		case inPipelines && indent == 6 && cur != "":
			k, v, _ := strings.Cut(trimmed, ":")
			for _, item := range strings.Split(strings.Trim(strings.TrimSpace(v), "[]"), ",") {
				out[cur][k] = append(out[cur][k], strings.TrimSpace(item))
			}
		}
	}
	return out
}

// INV-5 — the hook allowlist in the collector is exactly the pinned list, and holds no content.
func TestINV5_HookAllowlistIsPinned(t *testing.T) {
	m := regexp.MustCompile(`(?s)keep_keys\(log\.cache, \[(.*?)\]\)`).FindStringSubmatch(collectorConfig(t))
	if m == nil {
		t.Fatal("no keep_keys(log.cache, [...]) in deploy/collector/config.yaml.\n" +
			"  Without it every hook payload reaches storage whole: prompt text, command lines,\n" +
			"  file contents and paths (INV-5).")
	}
	var got []string
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
		got = append(got, q[1])
	}
	want := append([]string(nil), hookAllowlist...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the collector's hook allowlist differs from the pinned one.\n"+
			"  collector: %v\n  pinned:    %v\n"+
			"  Keeping a new hook field is adding to the mandatory tier: write the ADR first (INV-4),\n"+
			"  then change both lists.", got, want)
	}
	for _, f := range hookContentFields {
		for _, k := range got {
			if k == f {
				t.Errorf("INV-5: %q carries content or a path and must never be on the hook allowlist", f)
			}
		}
	}
}

// A generic hook field is read only for the one event whose meaning of it is known. "source" on
// ConfigChange is a settings scope; on some future event it could be anything.
func TestCollectorScopesGenericHookFields(t *testing.T) {
	generic := regexp.MustCompile(`log\.cache\["(reason|trigger|source|prompt|message|error)"\]`)
	for _, line := range strings.Split(collectorConfig(t), "\n") {
		if generic.MatchString(line) && !strings.Contains(line, `log.cache["hook_event_name"] ==`) {
			t.Errorf("a statement reads a generic hook field without naming the event it applies to:\n  %s",
				strings.TrimSpace(line))
		}
	}
}

// The strict naming mode fails closed: anything but the literal "all" redacts, so a typo in
// CARDO_ARTIFACT_NAMES cannot quietly keep every name.
func TestCollectorStrictNamesFailClosed(t *testing.T) {
	cfg := collectorConfig(t)
	all := strings.Count(cfg, "${env:CARDO_ARTIFACT_NAMES")
	closed := strings.Count(cfg, `"${env:CARDO_ARTIFACT_NAMES:-all}" != "all"`)
	if all == 0 {
		t.Fatal("no statement reads CARDO_ARTIFACT_NAMES; the strict naming mode is gone (ADR-0025)")
	}
	if all != closed {
		t.Errorf("%d of %d uses of CARDO_ARTIFACT_NAMES are not the fail-closed form "+
			`"${env:CARDO_ARTIFACT_NAMES:-all}" != "all"`, all-closed, all)
	}
}

// A blank CARDO_ORG_REPOS classifies every repository as external (ADR-0035). An empty regex
// matches every string, so each match on the pattern has to rule the blank value out itself, or a
// collector started without the variable would record every repository by name, customers' and
// side projects' included.
func TestCollectorBlankOrgReposIsExternal(t *testing.T) {
	cfg := collectorConfig(t)
	uses := strings.Count(cfg, "${env:CARDO_ORG_REPOS")
	guarded := regexp.MustCompile(
		`"\$\{env:CARDO_ORG_REPOS:-\}" != "" and IsMatch\([^,]+, "\$\{env:CARDO_ORG_REPOS:-\}"\)`).FindAllString(cfg, -1)
	if len(guarded) == 0 {
		t.Fatal("no statement classifies a repository by CARDO_ORG_REPOS; the classification is gone (ADR-0035)")
	}
	if uses != 2*len(guarded) {
		t.Errorf("CARDO_ORG_REPOS is used %d times, and only %d of them are in the guarded form "+
			`"${env:CARDO_ORG_REPOS:-}" != "" and IsMatch(x, "${env:CARDO_ORG_REPOS:-}")`,
			uses, 2*len(guarded))
	}
}

// The hook receiver's own limit is 100 KiB, refused with a 400 and no log line on the collector.
// Real payloads carry whole prompts and subagent replies and cross it, and the event is lost.
func TestCollectorAcceptsRealisticHookBodies(t *testing.T) {
	block := sectionBlocks(collectorConfig(t), "receivers")["webhook_event"]
	m := regexp.MustCompile(`(?m)^\s+max_request_body_size:\s*(\d+)\s*$`).FindStringSubmatch(block)
	if m == nil {
		t.Fatal("webhook_event sets no max_request_body_size, so its 100 KiB default applies and " +
			"large hook payloads are dropped without a trace")
	}
	var n int
	fmt.Sscan(m[1], &n)
	if n < 4<<20 {
		t.Errorf("webhook_event max_request_body_size is %d; keep it at 4 MiB or more", n)
	}
}

// The hook receiver's timeouts default to 500 ms, after which it closes the connection with no
// response: Claude Code shows "socket hang up", and late headers lose the event. The receiver
// ignores a key it does not know, so a misspelt one is silent; TestCollector_SlowHookSendersAreAnswered
// checks the running collector.
func TestCollectorWaitsForSlowHookSenders(t *testing.T) {
	block := sectionBlocks(collectorConfig(t), "receivers")["webhook_event"]
	for _, key := range []string{"read_timeout", "write_timeout"} {
		m := regexp.MustCompile(`(?m)^\s+` + key + `:\s*(\S+)\s*$`).FindStringSubmatch(block)
		if m == nil {
			t.Errorf("webhook_event sets no %s, so its 500 ms default applies and a slow hook is "+
				"answered with a closed connection", key)
			continue
		}
		if d, err := time.ParseDuration(m[1]); err != nil || d < 5*time.Second {
			t.Errorf("webhook_event %s is %q; keep it at 5s or more (the receiver allows up to 10s)", key, m[1])
		}
	}
}

// Every processor fails closed. With error_mode: ignore, a statement that errors is skipped and
// the record carries on -- so a hashing statement that failed would let the email through
// unhashed. propagate refuses the batch instead.
func TestCollectorProcessorsFailClosed(t *testing.T) {
	blocks := sectionBlocks(collectorConfig(t), "processors")
	if len(blocks) == 0 {
		t.Fatal("found no processors; the config parser is wrong or the config is")
	}
	for name, body := range blocks {
		if !strings.Contains(body, "error_mode: propagate") {
			t.Errorf("processor %s does not set error_mode: propagate.\n"+
				"  Under the default, a statement that fails is skipped and the record is exported\n"+
				"  anyway -- for the identity transform that means an unhashed email (INV-2).", name)
		}
	}
}

// Every pipeline ends in the INV-2 tripwire, and every pipeline carrying identity starts by
// refusing a weak salt and then hashing, before anything else can look at the record.
func TestCollectorPipelinesOrderTheirSafeguards(t *testing.T) {
	ps := pipelines(collectorConfig(t))
	for _, name := range []string{"logs/hooks", "logs/otel", "metrics/otel"} {
		p, ok := ps[name]
		if !ok {
			t.Errorf("pipeline %s is missing", name)
			continue
		}
		procs := p["processors"]
		if len(procs) == 0 || procs[len(procs)-1] != "filter/inv2_tripwire" {
			t.Errorf("pipeline %s does not end with filter/inv2_tripwire: %v", name, procs)
		}
		if name == "logs/hooks" {
			if len(procs) == 0 || procs[0] != "transform/hooks" {
				t.Errorf("logs/hooks must start with transform/hooks, so nothing sees a raw payload: %v", procs)
			}
			continue
		}
		if len(procs) < 2 || procs[0] != "transform/refuse_weak_salt" || procs[1] != "transform/otel_identity" {
			t.Errorf("pipeline %s must start with transform/refuse_weak_salt, transform/otel_identity: %v",
				name, procs)
		}
	}
	for name := range ps {
		if name != "logs/hooks" && name != "logs/otel" && name != "metrics/otel" {
			t.Errorf("unexpected pipeline %s. A new pipeline needs the same safeguards as the others, "+
				"and this test updated to check them.", name)
		}
	}
}

// INV-7 — the collector sends data nowhere but the organization's own ClickHouse. An otlp or otlphttp
// exporter is the one-line change that would forward every event somewhere else.
func TestINV7_CollectorExportsOnlyToClickHouse(t *testing.T) {
	cfg := collectorConfig(t)
	exporters := sectionBlocks(cfg, "exporters")
	if len(exporters) == 0 {
		t.Fatal("found no exporters; the config parser is wrong or the config is")
	}
	for name := range exporters {
		if !strings.HasPrefix(name, "clickhouse") {
			t.Errorf("INV-7: exporter %q is not a ClickHouse exporter. No data leaves the organization's "+
				"network; forwarding needs an ADR.", name)
		}
	}
	if len(sectionBlocks(cfg, "extensions")) > 0 {
		t.Error("INV-7: the collector declares extensions. opamp and friends phone home to a " +
			"management server; none are needed here.")
	}
	if !regexp.MustCompile(`(?m)^    metrics:\n      level: none$`).MatchString(cfg) {
		t.Error("the collector's own metrics must stay off (service.telemetry.metrics.level: none)")
	}
}

// ---- Live tests ---------------------------------------------------------------------------

type liveStack struct {
	hooksURL, otlpURL string
	ch                *clickhouse.Client
	salt              string
	orgArtifacts      *regexp.Regexp
	strictNames       bool
	orgRepos          *regexp.Regexp
}

func liveCollector(t *testing.T) *liveStack {
	t.Helper()
	s := &liveStack{
		hooksURL: os.Getenv("CARDO_COLLECTOR_HOOKS_URL"),
		otlpURL:  strings.TrimRight(os.Getenv("CARDO_COLLECTOR_OTLP_URL"), "/"),
		salt:     os.Getenv("CARDO_SALT"),
	}
	chURL := os.Getenv("CARDO_CLICKHOUSE_URL")
	if s.hooksURL == "" || s.otlpURL == "" || chURL == "" || s.salt == "" {
		t.Skip("set CARDO_COLLECTOR_HOOKS_URL, CARDO_COLLECTOR_OTLP_URL, CARDO_CLICKHOUSE_URL and " +
			"CARDO_SALT to run the live collector tests (make collector-test)")
	}
	// Mirror the collector: an unset or empty pattern names no organization artifacts, and any
	// naming mode but "all" is strict.
	if pattern := os.Getenv("CARDO_ORG_ARTIFACTS"); pattern != "" {
		s.orgArtifacts = regexp.MustCompile(pattern)
	}
	if mode := os.Getenv("CARDO_ARTIFACT_NAMES"); mode != "" && mode != "all" {
		s.strictNames = true
	}
	// An unset or empty CARDO_ORG_REPOS makes every repository external.
	if pattern := os.Getenv("CARDO_ORG_REPOS"); pattern != "" {
		s.orgRepos = regexp.MustCompile(pattern)
	}

	c, err := clickhouse.NewClient(clickhouse.Config{
		Endpoint: chURL,
		User:     os.Getenv("CARDO_CLICKHOUSE_USER"),
		Password: os.Getenv("CARDO_CLICKHOUSE_PASSWORD"),
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.ch = c
	t.Cleanup(func() { c.Close() })
	return s
}

func (s *liveStack) post(t *testing.T, url string, body []byte) int {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// rows waits for at least want rows matching where, then returns them all. The exporter flushes
// on a 10 s timer, so this has to poll.
func (s *liveStack) rows(t *testing.T, table, cols, where string, want int) []map[string]any {
	t.Helper()
	ctx := context.Background()
	// Registered before polling, not after: a run that fails while waiting must not leave its
	// rows behind in a stack someone is about to point their own Claude Code at.
	t.Cleanup(func() {
		s.ch.Exec(context.Background(), fmt.Sprintf("DELETE FROM cardo.%s WHERE %s", table, where))
	})
	deadline := time.Now().Add(45 * time.Second)
	for {
		out, err := s.ch.Query(ctx, fmt.Sprintf("SELECT count() FROM cardo.%s WHERE %s", table, where))
		if err != nil {
			t.Fatal(err)
		}
		var n int
		fmt.Sscan(strings.TrimSpace(string(out)), &n)
		if n >= want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d rows reached cardo.%s within 45s (where %s).\n"+
				"  Is the collector running, pointed at this ClickHouse, and started with this CARDO_SALT?",
				n, want, table, where)
		}
		time.Sleep(time.Second)
	}
	out, err := s.ch.Query(ctx, fmt.Sprintf("SELECT %s FROM cardo.%s WHERE %s FORMAT JSONEachRow", cols, table, where))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decoding %q: %v", line, err)
		}
		rows = append(rows, r)
	}
	return rows
}

func stringMap(v any) map[string]string {
	out := map[string]string{}
	m, _ := v.(map[string]any)
	for k, val := range m {
		out[k], _ = val.(string)
	}
	return out
}

func assertNoMarkers(t *testing.T, what string, row map[string]any) {
	t.Helper()
	b, _ := json.Marshal(row)
	text := strings.ToLower(string(b))
	for _, m := range contentMarkers {
		if strings.Contains(text, strings.ToLower(m)) {
			t.Errorf("INV-5: %s stored content marker %q:\n  %s", what, m, b)
		}
	}
}

// hookFixtureDirs are replayed together. 2.1.281 holds the key sets observed from a real Claude
// Code; documented holds what the documentation said before that, still accepted so an older
// client keeps working, and the two events not yet observed (SessionStart, PermissionDenied).
var hookFixtureDirs = []string{"2.1.281", "documented"}

// Every observed and documented hook shape goes in carrying content; only allowlisted,
// content-free fields come out, and the facts Cardo derives from content are right.
func TestCollector_HookPayloadsReduceToTheAllowlist(t *testing.T) {
	s := liveCollector(t)
	run := fmt.Sprintf("cardo-test-%d", time.Now().UnixNano())
	// Before the first POST, not after: a run that stops while sending must not leave rows behind.
	t.Cleanup(func() {
		s.ch.Exec(context.Background(), fmt.Sprintf(
			"DELETE FROM cardo.bronze_hook_events WHERE startsWith(LogAttributes['session_id'], '%s.')", run))
	})

	sent := map[string]map[string]any{}
	send := func(key string, body map[string]any) {
		t.Helper()
		sid := run + "." + key
		body["session_id"] = sid
		sent[sid] = body
		out, _ := json.Marshal(body)
		if code := s.post(t, s.hooksURL, out); code != http.StatusOK {
			t.Fatalf("%s: collector answered %d", key, code)
		}
	}
	for _, dir := range hookFixtureDirs {
		files, _ := filepath.Glob(filepath.Join(repoRoot(t), "test", "fixtures", "hooks", dir, "*.json"))
		if len(files) < 11 {
			t.Fatalf("expected a fixture per hook event in test/fixtures/hooks/%s, found %d", dir, len(files))
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(b, &body); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			send(dir+"/"+strings.TrimSuffix(filepath.Base(f), ".json"), body)
		}
	}

	// Past the receiver's 100 KiB default: a subagent's whole reply, as real ones are. The event
	// must still arrive, without the reply.
	large := map[string]any{}
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "test", "fixtures", "hooks", "2.1.281", "SubagentStop.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &large)
	large["last_assistant_message"] = "CONTENT-MARKER " + strings.Repeat("a long reply ", 25000)
	send("large/SubagentStop", large)

	// A body that is not JSON must be refused, not stored raw.
	if code := s.post(t, s.hooksURL, []byte("CONTENT-MARKER this is not json")); code == http.StatusOK {
		t.Error("a non-JSON hook body was accepted; it must be refused rather than stored")
	}

	allowed := map[string]bool{"cardo.received_keys": true}
	for _, k := range hookAllowlist {
		allowed[k] = true
	}

	rows := s.rows(t, "bronze_hook_events", "EventName, Body, LogAttributes, ResourceAttributes",
		fmt.Sprintf("startsWith(LogAttributes['session_id'], '%s.')", run), len(sent))
	if len(rows) != len(sent) {
		t.Errorf("sent %d hook payloads, stored %d rows", len(sent), len(rows))
	}

	events := map[string]bool{}
	for _, r := range rows {
		attrs := stringMap(r["LogAttributes"])
		res := stringMap(r["ResourceAttributes"])
		sid := attrs["session_id"]
		name := strings.TrimPrefix(sid, run+".")
		in := sent[sid]
		events[attrs["hook_event_name"]] = true

		assertNoMarkers(t, name, r)
		if body, _ := r["Body"].(string); body != "" {
			t.Errorf("%s: Body is not empty; the raw payload reached storage: %q", name, body)
		}
		for k := range attrs {
			if !allowed[k] {
				t.Errorf("%s: stored attribute %q is not on the hook allowlist", name, k)
			}
		}
		if r["EventName"] != in["hook_event_name"] {
			t.Errorf("%s: EventName = %v, want %v", name, r["EventName"], in["hook_event_name"])
		}
		if res["cardo.tier"] != "1" || res["cardo.salt_version"] == "" {
			t.Errorf("%s: tier / salt_version not tagged: %v", name, res)
		}
		// Every received key is named, whether or not it was kept -- that is the drift signal.
		for k := range in {
			if !strings.Contains(attrs["cardo.received_keys"], `"`+k+`"`) {
				t.Errorf("%s: received key %q is missing from cardo.received_keys (%s)",
					name, k, attrs["cardo.received_keys"])
			}
		}

		want := map[string]string{}
		switch name {
		// Observed shapes, Claude Code 2.1.281.
		case "2.1.281/UserPromptSubmit":
			want["prompt_length"] = fmt.Sprint(len(in["prompt"].(string)))
			want["permission_mode"] = "auto"
		case "2.1.281/UserPromptExpansion":
			want["command_name"] = nameAsStored(s, "acme-release-notes")
			want["command_source"], want["expansion_type"] = "projectSettings", "slash_command"
			want["prompt_length"] = "" // the expanded prompt is not what the engineer typed
		case "2.1.281/UserPromptExpansion.personal":
			want["command_name"] = nameAsStored(s, "standup")
		case "2.1.281/SessionEnd":
			want["session_end_reason"] = "prompt_input_exit"
		case "2.1.281/SessionEnd.prose-reason":
			want["session_end_reason"] = ""
		case "2.1.281/PreCompact", "2.1.281/PostCompact":
			want["compaction_reason"] = "manual"
		case "2.1.281/ConfigChange":
			want["config_source"] = "local_settings"
			want["instructions_file"] = "" // a settings file is not an instructions file
		case "2.1.281/InstructionsLoaded.user":
			want["instructions_file"], want["memory_type"], want["load_reason"] = "CLAUDE.md", "User", "session_start"
			want["instructions_name"] = ""
		case "2.1.281/InstructionsLoaded.nested":
			want["instructions_file"], want["load_reason"] = "CLAUDE.md", "nested_traversal"
		case "2.1.281/InstructionsLoaded.org-rule":
			want["instructions_file"] = "rule"
			want["instructions_name"] = ""
			if s.orgArtifacts != nil && s.orgArtifacts.MatchString("acme-security-v3.md") {
				want["instructions_name"] = "acme-security-v3.md"
			}
		case "2.1.281/InstructionsLoaded.include":
			// A file a CLAUDE.md imports with @. Its parent's path goes the way of every path.
			want["instructions_file"], want["memory_type"], want["load_reason"] = "other", "Project", "include"
			want["instructions_name"] = ""
			if s.orgArtifacts != nil && s.orgArtifacts.MatchString("acme-standards-v7.md") {
				want["instructions_name"] = "acme-standards-v7.md"
			}
		case "2.1.281/InstructionsLoaded.managed-windows":
			want["instructions_file"], want["instructions_scope"] = "CLAUDE.md", "managed"
		case "2.1.281/PermissionRequest":
			want["tool_name"], want["effort_level"], want["tool_use_id"] = "Edit", "xhigh", ""
		case "2.1.281/PermissionRequest.subagent":
			want["tool_name"], want["agent_type"] = "Bash", "Explore"
		case "2.1.281/PermissionRequest.mcp", "documented/PermissionRequest.mcp":
			want["tool_name"] = mcpAsStored(s, "mcp__personal-notes__search")
		case "2.1.281/SubagentStart":
			want["agent_type"] = "Explore"
		case "2.1.281/SubagentStop", "large/SubagentStop":
			want["agent_type"] = nameAsStored(s, "acme-security-reviewer")
		case "2.1.281/PreModelSwitch":
			want["from_model"], want["to_model"] = "claude-opus-5-5", "claude-sonnet-5"
			want["requested_model"], want["model_switch_source"] = "sonnet", "command"
			want["context_tokens"], want["prompt_cache_warm"] = "48213", "true"
			want["cache_ttl"], want["estimated_cache_write_usd"] = "1h", "0.18"
		case "2.1.281/PreModelSwitch.wrong-types":
			for _, k := range []string{"requested_model", "model_switch_source", "context_tokens",
				"prompt_cache_warm", "cache_ttl", "estimated_cache_write_usd"} {
				want[k] = ""
			}

		// Documented shapes: the names the documentation used are still understood.
		case "documented/UserPromptSubmit":
			want["prompt_length"] = fmt.Sprint(len(in["user_input"].(string)))
			want["permission_mode"] = "plan"
			want["effort_level"] = "high"
		case "documented/InstructionsLoaded.project":
			want["instructions_file"] = "CLAUDE.md"
			want["instructions_scope"] = ""
			want["content_hash"] = in["content_hash"].(string)
		case "documented/InstructionsLoaded.managed-windows":
			want["instructions_file"] = "CLAUDE.md"
			want["instructions_scope"] = "managed"
		case "documented/InstructionsLoaded.rule":
			want["instructions_file"] = "rule"
		case "documented/SubagentStart":
			want["agent_type"] = "Explore"
		case "documented/SubagentStop":
			want["agent_type"] = nameAsStored(s, "acme-security-reviewer")
		case "documented/SubagentStop.private-agent":
			want["agent_type"] = nameAsStored(s, "personal-helper-agent")
		case "documented/UserPromptExpansion":
			want["command_name"] = nameAsStored(s, "acme-release-notes")
		case "documented/PermissionRequest":
			want["tool_name"] = "Bash"
			want["tool_use_id"] = "toolu_01PERMREQ"
		case "documented/PreModelSwitch":
			want["from_model"], want["to_model"] = "claude-opus-4-6", "claude-opus-5"
		case "documented/SessionStart.drift":
			want["a_field_from_a_future_release"] = ""
		}
		for k, v := range want {
			if attrs[k] != v {
				t.Errorf("%s: %s = %q, want %q", name, k, attrs[k], v)
			}
		}
	}

	for _, e := range mandatoryHookEvents {
		if !events[e] {
			t.Errorf("no stored row for mandatory hook event %s", e)
		}
	}

	// The same rows must populate the canonical views. A collector change that renamed a field
	// the views read would pass every check above and silently empty a dashboard.
	wantKinds := map[string]int{}
	for _, in := range sent {
		switch in["hook_event_name"] {
		case "InstructionsLoaded":
			wantKinds["instructions"]++
		case "SubagentStart":
			wantKinds["subagent"]++
		case "UserPromptExpansion":
			wantKinds["skill"]++
		case "PermissionRequest":
			if name, _ := in["tool_name"].(string); strings.HasPrefix(name, "mcp__") {
				wantKinds["mcp"]++
			}
		}
	}
	count := func(q string) map[string]int {
		out, err := s.ch.Query(context.Background(), q+" FORMAT TabSeparated")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			var k string
			var n int
			fmt.Sscanf(strings.ReplaceAll(line, "\t", " "), "%s %d", &k, &n)
			got[k] = n
		}
		return got
	}
	sessions := count(fmt.Sprintf(
		"SELECT 'sessions', count() FROM cardo.silver_session WHERE startsWith(session_id, '%s.')", run))
	if sessions["sessions"] != len(sent) {
		t.Errorf("silver_session holds %d of the %d sessions the fixtures opened", sessions["sessions"], len(sent))
	}
	kinds := count(fmt.Sprintf(
		"SELECT kind, count() FROM cardo.silver_artifact_load WHERE startsWith(session_id, '%s.') GROUP BY kind", run))
	for kind, n := range wantKinds {
		if kinds[kind] != n {
			t.Errorf("silver_artifact_load holds %d %s rows from the fixtures, want %d", kinds[kind], kind, n)
		}
	}
}

// nameAsStored is what the collector should store for a user-defined command or subagent name:
// the name itself, unless the strict mode is on and the name is not the organization's.
func nameAsStored(s *liveStack, name string) string {
	if !s.strictNames || (s.orgArtifacts != nil && s.orgArtifacts.MatchString(name)) {
		return name
	}
	return "custom"
}

func mcpAsStored(s *liveStack, name string) string {
	if nameAsStored(s, name) == "custom" {
		return "mcp__custom"
	}
	return name
}

func otlpAttrs(kv ...string) []map[string]any {
	var out []map[string]any
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, map[string]any{"key": kv[i], "value": map[string]any{"stringValue": kv[i+1]}})
	}
	return out
}

// INV-2 across both collection paths: the collector's pseudonym for an engineer is exactly the
// one the poller computes, on every place identity can appear, and nothing else identifying
// survives.
func TestCollector_OTelIdentityIsThePollersPseudonym(t *testing.T) {
	s := liveCollector(t)
	session := fmt.Sprintf("cardo-test-otel-%d", time.Now().UnixNano())
	now := fmt.Sprint(time.Now().UnixNano())

	// Mixed case and stray whitespace: the poller normalizes both, so the collector must too, or
	// one engineer becomes two.
	email := "  Marker.Person@Example-Corp.com "
	h, err := pseudonym.New(s.salt, "v1")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := h.Hash(email)

	identifying := []string{
		"user.email", email, "user.account_uuid", "acct-marker-user", "user.account_id", "user_01MARKER",
		"user.id", "install-marker-user",
	}
	resource := map[string]any{"attributes": otlpAttrs(append([]string{
		"service.name", "claude-code", "host.name", "marker-user-laptop", "cardo.cohort", "payments",
		"vcs.repository.url.full", "https://gitlab.com/marker-user/secret",
	}, identifying...)...)}

	logs := map[string]any{"resourceLogs": []any{map[string]any{
		"resource": resource,
		"scopeLogs": []any{map[string]any{"scope": map[string]any{"name": "com.anthropic.claude_code.events"},
			"logRecords": []any{map[string]any{
				"timeUnixNano": now,
				"body":         map[string]any{"stringValue": "CONTENT-MARKER a body is never trusted"},
				"attributes": otlpAttrs(append([]string{
					"event.name", "user_prompt", "session.id", session, "prompt_length", "42",
					"prompt", "CONTENT-MARKER the prompt text", "error", "CONTENT-MARKER /home/marker-user/x",
					"workspace.host_paths", "/home/marker-user/src",
					// A personal command's name, which Claude Code sends on skill.name even with every
					// content flag off.
					"skill.name", "standup-notes",
				}, identifying...)...),
			}}}}}}}
	metrics := map[string]any{"resourceMetrics": []any{map[string]any{
		"resource": resource,
		"scopeMetrics": []any{map[string]any{"scope": map[string]any{"name": "com.anthropic.claude_code"},
			"metrics": []any{map[string]any{"name": "claude_code.cost.usage", "unit": "USD",
				"sum": map[string]any{"aggregationTemporality": 1, "isMonotonic": true,
					"dataPoints": []any{map[string]any{
						"asDouble": 0.25, "startTimeUnixNano": now, "timeUnixNano": now,
						"attributes": otlpAttrs(append([]string{
							"session.id", session, "model", "claude-opus-5", "skill.name", "acme-review",
						}, identifying...)...),
					}}}}}}}}}}

	for path, body := range map[string]any{"/v1/logs": logs, "/v1/metrics": metrics} {
		b, _ := json.Marshal(body)
		if code := s.post(t, s.otlpURL+path, b); code != http.StatusOK {
			t.Fatalf("POST %s answered %d -- a 503 with REFUSED in the collector log means its "+
				"CARDO_SALT is missing or short", path, code)
		}
	}

	forbidden := []string{"user.email", "user.account_uuid", "user.account_id", "user.id", "host.name",
		"prompt", "error", "workspace.host_paths", "vcs.repository.url.full"}
	check := func(what string, attrs map[string]string) {
		t.Helper()
		if attrs["user.pseudonym"] != want {
			t.Errorf("%s: user.pseudonym = %q, want the poller's %q.\n"+
				"  If these differ, one engineer is two people depending on which path saw them.\n"+
				"  Check the collector was started with this CARDO_SALT.", what, attrs["user.pseudonym"], want)
		}
		for _, k := range forbidden {
			if _, ok := attrs[k]; ok {
				t.Errorf("%s: %q reached storage", what, k)
			}
		}
	}

	lr := s.rows(t, "bronze_otel_logs", "EventName, Body, LogAttributes, ResourceAttributes",
		fmt.Sprintf("LogAttributes['session.id'] = '%s'", session), 1)
	for _, r := range lr {
		assertNoMarkers(t, "otel log", r)
		check("log attributes", stringMap(r["LogAttributes"]))
		check("log resource", stringMap(r["ResourceAttributes"]))
		if b, _ := r["Body"].(string); b != "" {
			t.Errorf("otel log Body not emptied: %q", b)
		}
		if r["EventName"] != "claude_code.user_prompt" {
			t.Errorf("EventName = %v, want claude_code.user_prompt", r["EventName"])
		}
		if a := stringMap(r["LogAttributes"]); a["prompt_length"] != "42" {
			t.Errorf("prompt_length was not kept: %v", a)
		}
		if a := stringMap(r["LogAttributes"]); a["skill.name"] != nameAsStored(s, "standup-notes") {
			t.Errorf("skill.name = %q, want %q (ADR-0025)", a["skill.name"], nameAsStored(s, "standup-notes"))
		}
		if res := stringMap(r["ResourceAttributes"]); res["cardo.cohort"] != "payments" {
			t.Errorf("the cohort attribute was not kept: %v", res)
		}
	}

	mr := s.rows(t, "bronze_otel_metrics_sum", "MetricName, Attributes, ResourceAttributes",
		fmt.Sprintf("Attributes['session.id'] = '%s'", session), 1)
	for _, r := range mr {
		assertNoMarkers(t, "otel metric", r)
		check("datapoint attributes", stringMap(r["Attributes"]))
		check("metric resource", stringMap(r["ResourceAttributes"]))
		if a := stringMap(r["Attributes"]); a["skill.name"] != "acme-review" {
			t.Errorf("skill.name was not kept: %v", a)
		}
	}
}

// ADR-0035: a repository is recorded by name only when it is the organization's. Any other is
// reduced to its host, a blank CARDO_ORG_REPOS makes every repository external, and the URL itself
// is never stored. The form Claude Code sends the URL in has not been observed, so every form a git
// remote can take is sent, on the resource and on the record, and each must reduce to the same
// host/owner/name before it is matched.
func TestCollector_RepositoriesAreClassified(t *testing.T) {
	s := liveCollector(t)
	run := fmt.Sprintf("cardo-test-repo-%d", time.Now().UnixNano())
	now := fmt.Sprint(time.Now().UnixNano())

	cases := []struct{ url, path, host string }{
		{"https://GitHub.com/Acme/Widgets", "github.com/acme/widgets", "github.com"},
		{"git@github.com:acme/widgets.git", "github.com/acme/widgets", "github.com"},
		{"ssh://git@github.com:22/acme/tools.git/", "github.com/acme/tools", "github.com"},
		{"https://x-access-token:CONTENT-MARKER@github.com/acme/infra?ref=CONTENT-MARKER", "github.com/acme/infra", "github.com"},
		{"https://gitlab.com/marker-user/side-project", "gitlab.com/marker-user/side-project", "gitlab.com"},
		{"file:///home/marker-user/scratch", "/home/marker-user/scratch", ""},
		// A session outside any repository, carrying a class of its own making.
		{"", "", ""},
	}
	want := func(i int) (class, repo string, ok bool) {
		c := cases[i]
		switch {
		case c.url == "":
			return "", "", false
		case s.orgRepos != nil && s.orgRepos.MatchString(c.path):
			return "org", c.path, true
		default:
			return "external", c.host, true
		}
	}
	if s.orgRepos == nil {
		t.Log("CARDO_ORG_REPOS is unset: every repository should be external")
	}

	var resourceLogs, resourceMetrics []any
	for i, c := range cases {
		session := fmt.Sprintf("%s.%d", run, i)
		vcs := []string{"vcs.repository.url.full", c.url, "vcs.owner.name", "marker-user",
			"vcs.repository.name", "marker-user-repo", "vcs.provider.name", "github"}
		if c.url == "" {
			vcs = []string{"cardo.repo.class", "org", "cardo.repo", "gitlab.com/marker-user/x"}
		}
		resource := map[string]any{"attributes": otlpAttrs(append([]string{"service.name", "claude-code"}, vcs...)...)}
		resourceLogs = append(resourceLogs, map[string]any{
			"resource": resource,
			"scopeLogs": []any{map[string]any{"scope": map[string]any{"name": "com.anthropic.claude_code.events"},
				"logRecords": []any{map[string]any{"timeUnixNano": now,
					"attributes": otlpAttrs(append([]string{"event.name", "tool_result", "session.id", session}, vcs...)...),
				}}}}})
		resourceMetrics = append(resourceMetrics, map[string]any{
			"resource": resource,
			"scopeMetrics": []any{map[string]any{"scope": map[string]any{"name": "com.anthropic.claude_code"},
				"metrics": []any{map[string]any{"name": "claude_code.cost.usage", "unit": "USD",
					"sum": map[string]any{"aggregationTemporality": 1, "isMonotonic": true,
						"dataPoints": []any{map[string]any{
							"asDouble": 0.01, "startTimeUnixNano": now, "timeUnixNano": now,
							"attributes": otlpAttrs(append([]string{"session.id", session}, vcs...)...),
						}}}}}}}})
	}
	for path, body := range map[string]any{
		"/v1/logs":    map[string]any{"resourceLogs": resourceLogs},
		"/v1/metrics": map[string]any{"resourceMetrics": resourceMetrics},
	} {
		b, _ := json.Marshal(body)
		if code := s.post(t, s.otlpURL+path, b); code != http.StatusOK {
			t.Fatalf("POST %s answered %d", path, code)
		}
	}

	check := func(what, session string, attrs map[string]string) {
		t.Helper()
		var i int
		fmt.Sscanf(strings.TrimPrefix(session, run+"."), "%d", &i)
		for k := range attrs {
			if strings.HasPrefix(k, "vcs.") {
				t.Errorf("%s of %q: %q reached storage", what, cases[i].url, k)
			}
		}
		class, okClass := attrs["cardo.repo.class"]
		repo, okRepo := attrs["cardo.repo"]
		wantClass, wantRepo, ok := want(i)
		if !ok {
			if okClass || okRepo {
				t.Errorf("%s with no repository: stored cardo.repo.class=%q cardo.repo=%q, want neither",
					what, class, repo)
			}
			return
		}
		if class != wantClass || repo != wantRepo || !okRepo {
			t.Errorf("%s of %q: stored cardo.repo.class=%q cardo.repo=%q, want %q and %q",
				what, cases[i].url, class, repo, wantClass, wantRepo)
		}
	}
	for _, r := range s.rows(t, "bronze_otel_logs", "LogAttributes, ResourceAttributes",
		fmt.Sprintf("startsWith(LogAttributes['session.id'], '%s.')", run), len(cases)) {
		assertNoMarkers(t, "otel log", r)
		a := stringMap(r["LogAttributes"])
		check("log attributes", a["session.id"], a)
		check("log resource", a["session.id"], stringMap(r["ResourceAttributes"]))
	}
	for _, r := range s.rows(t, "bronze_otel_metrics_sum", "Attributes, ResourceAttributes",
		fmt.Sprintf("startsWith(Attributes['session.id'], '%s.')", run), len(cases)) {
		assertNoMarkers(t, "otel metric", r)
		a := stringMap(r["Attributes"])
		check("datapoint attributes", a["session.id"], a)
		check("metric resource", a["session.id"], stringMap(r["ResourceAttributes"]))
	}
}

// INV-2 defence in depth: an email in an attribute the transforms have never heard of is caught
// by the tripwire and the record dropped, while its clean neighbour is stored.
func TestINV2_CollectorTripwireDropsWhatTheTransformsMissed(t *testing.T) {
	s := liveCollector(t)
	run := fmt.Sprintf("cardo-test-trip-%d", time.Now().UnixNano())
	now := fmt.Sprint(time.Now().UnixNano())
	rec := func(sid string, extra ...string) map[string]any {
		return map[string]any{"timeUnixNano": now,
			"attributes": otlpAttrs(append([]string{"event.name", "tool_result", "session.id", sid}, extra...)...)}
	}
	logs := map[string]any{"resourceLogs": []any{map[string]any{
		"resource": map[string]any{"attributes": otlpAttrs("service.name", "claude-code")},
		"scopeLogs": []any{map[string]any{"scope": map[string]any{"name": "t"}, "logRecords": []any{
			rec(run + ".clean"),
			rec(run+".leak", "some.future.owner", "marker.person@example-corp.com"),
		}}}}}}
	b, _ := json.Marshal(logs)
	t.Cleanup(func() {
		s.ch.Exec(context.Background(), fmt.Sprintf(
			"DELETE FROM cardo.bronze_otel_logs WHERE LogAttributes['session.id'] = '%s.leak'", run))
	})
	if code := s.post(t, s.otlpURL+"/v1/logs", b); code != http.StatusOK {
		t.Fatalf("collector answered %d", code)
	}

	// Both records travel in one request and one batch, so once the clean one has landed the
	// leaky one has had every chance to.
	s.rows(t, "bronze_otel_logs", "LogAttributes", fmt.Sprintf("LogAttributes['session.id'] = '%s.clean'", run), 1)
	out, err := s.ch.Query(context.Background(), fmt.Sprintf(
		"SELECT count() FROM cardo.bronze_otel_logs WHERE LogAttributes['session.id'] = '%s.leak'", run))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.TrimSpace(string(out)); n != "0" {
		t.Errorf("INV-2: a record carrying an email in an unknown attribute was stored (%s rows).\n"+
			"  filter/inv2_tripwire must drop anything the transforms did not scrub.", n)
	}
}

// A Claude Code running several hooks at once can be slow to send one: a real UserPromptSubmit,
// sent beside two PowerShell hooks on Windows, was. Under the receiver's 500 ms defaults the
// collector closes the connection with no response, Claude Code shows "socket hang up" under the
// prompt, and late headers lose the event. Each request here pauses for longer than that.
func TestCollector_SlowHookSendersAreAnswered(t *testing.T) {
	s := liveCollector(t)
	u, err := url.Parse(s.hooksURL)
	if err != nil || u.Scheme != "http" {
		t.Fatalf("CARDO_COLLECTOR_HOOKS_URL must be the collector's own http:// address, not %q: "+
			"this test writes the request by hand", s.hooksURL)
	}
	run := fmt.Sprintf("cardo-test-slow-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		s.ch.Exec(context.Background(), fmt.Sprintf(
			"DELETE FROM cardo.bronze_hook_events WHERE startsWith(LogAttributes['session_id'], '%s.')", run))
	})

	const pause = 1500 * time.Millisecond
	send := func(late string) {
		t.Helper()
		body := fmt.Sprintf(`{"session_id":%q,"hook_event_name":"UserPromptSubmit","prompt":"slow sender"}`,
			run+"."+late)
		conn, err := net.Dial("tcp", u.Host)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(20 * time.Second))
		fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\n", u.Path, u.Host)
		if late == "headers" {
			time.Sleep(pause)
		}
		fmt.Fprintf(conn, "Content-Length: %d\r\n\r\n", len(body))
		if late == "body" {
			time.Sleep(pause)
		}
		fmt.Fprint(conn, body)
		status, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil || !strings.HasPrefix(status, "HTTP/1.1 200") {
			t.Errorf("a hook whose %s arrived %v late got %q (%v) instead of a 200.\n"+
				"  webhook_event's read_timeout and write_timeout are too short: Claude Code reports "+
				"\"socket hang up\".", late, pause, strings.TrimSpace(status), err)
		}
	}
	send("headers")
	send("body")
	s.rows(t, "bronze_hook_events", "LogAttributes",
		fmt.Sprintf("startsWith(LogAttributes['session_id'], '%s.')", run), 2)
}
