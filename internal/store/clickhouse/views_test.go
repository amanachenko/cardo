package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The collector-path views (sql/clickhouse/005 to 008), against a real ClickHouse.
//
// These seed the collector's bronze tables directly, with the attribute names a real Claude Code
// 2.1.281 sends, and read back silver and gold. The collector itself is tested in test/; what is
// tested here is the SQL: the joins across two clocks, and the group-size rule that decides what
// a dashboard may name (ADR-0029).
//
// Seven invented people, all on one day eight or nine weeks back, so gold's fleet-wide weekly and daily
// rows hold nothing else on a machine that also holds real sessions. The views read
// cardo.settings, which is shared, so the tests set it for their run and put back what was
// there.

const (
	testOrgPattern = "^acme-"
	logsTable      = Database + ".bronze_otel_logs"
	hooksTable     = Database + ".bronze_hook_events"
)

type seed struct {
	t     *testing.T
	st    *Store
	run   string
	day   time.Time
	logs  bytes.Buffer
	hooks bytes.Buffer
	// release is the Claude Code version person p runs, when it is not 2.1.281.
	release map[int]string
	// workflow names person p's job when p is a service run (ADR-0036), labelled as the CI
	// settings block labels it.
	workflow map[int]string
}

func newSeed(t *testing.T, st *Store, weeksBack int) *seed {
	t.Helper()
	now := time.Now().UTC()
	monday := now.Truncate(24*time.Hour).AddDate(0, 0, -int((now.Weekday()+6)%7))
	s := &seed{t: t, st: st, run: fmt.Sprintf("v%d", now.UnixNano()%1_000_000_000), day: monday.AddDate(0, 0, -7*weeksBack)}
	// Registered before anything is written, so a failed run leaves nothing behind.
	t.Cleanup(func() {
		ctx := context.Background()
		st.c.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE startsWith(LogAttributes['session.id'], 's-%s-')", logsTable, s.run))
		st.c.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE startsWith(LogAttributes['session_id'], 's-%s-')", hooksTable, s.run))
	})
	return s
}

// at is a time on the seeded day: 10:00 UTC plus an offset.
func (s *seed) at(offset time.Duration) time.Time { return s.day.Add(10*time.Hour + offset) }

func (s *seed) session(p int) string { return fmt.Sprintf("s-%s-%d", s.run, p) }
func (s *seed) person(p int) string  { return fmt.Sprintf("p-%s-%d", s.run, p) }
func (s *seed) prompt(p int, n string) string {
	return fmt.Sprintf("q-%s-%d-%s", s.run, p, n)
}

// cohort puts people 1 to 5 in one team, big enough to be named, and 6 and 7 in another that is not.
func (s *seed) cohort(p int) string {
	if p <= 5 {
		return s.run + "-big"
	}
	return s.run + "-small"
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.000000000") }

// otel adds a Claude Code OTel log event for person p, carrying identity as the collector stores it.
func (s *seed) otel(p int, at time.Time, event, prompt string, kv ...string) {
	attrs := map[string]string{
		"event.name": event, "session.id": s.session(p), "prompt.id": prompt,
		"user.pseudonym": s.person(p), "cardo.cohort": s.cohort(p), "app.version": "2.1.281",
	}
	if v := s.release[p]; v != "" {
		attrs["app.version"] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		attrs[kv[i]] = kv[i+1]
	}
	resource := map[string]string{"cardo.tier": "1", "cardo.salt_version": "v1", "cardo.cohort": s.cohort(p)}
	if wf := s.workflow[p]; wf != "" {
		resource["cardo.actor"], resource["cardo.workflow"] = "service", wf
	}
	json.NewEncoder(&s.logs).Encode(map[string]any{
		"Timestamp": stamp(at), "EventName": "claude_code." + event, "LogAttributes": attrs,
		"ResourceAttributes": resource,
	})
}

// hook adds a hook row for person p. It carries no identity, as a real one does not.
func (s *seed) hook(p int, at time.Time, event, prompt string, kv ...string) {
	attrs := map[string]string{"hook_event_name": event, "session_id": s.session(p), "prompt_id": prompt}
	for i := 0; i+1 < len(kv); i += 2 {
		attrs[kv[i]] = kv[i+1]
	}
	json.NewEncoder(&s.hooks).Encode(map[string]any{
		"Timestamp": stamp(at), "EventName": event, "LogAttributes": attrs,
		"ResourceAttributes": map[string]string{"cardo.tier": "1", "cardo.salt_version": "v1"},
	})
}

func (s *seed) request(p int, at time.Time, prompt, model string, input, cacheRead, cacheCreation int, cost string) {
	s.otel(p, at, "api_request", prompt, "query_source", "repl_main_thread", "model", model,
		"input_tokens", fmt.Sprint(input), "cache_read_tokens", fmt.Sprint(cacheRead),
		"cache_creation_tokens", fmt.Sprint(cacheCreation), "output_tokens", "50", "cost_usd", cost)
}

func (s *seed) write() {
	s.t.Helper()
	ctx := context.Background()
	if err := s.st.c.Insert(ctx, logsTable, s.logs.Bytes()); err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.c.Insert(ctx, hooksTable, s.hooks.Bytes()); err != nil {
		s.t.Fatal(err)
	}
	s.logs.Reset()
	s.hooks.Reset()
	// Silver is stored and rebuilt every 10 minutes (ADR-0052): rebuild it now, or the views read
	// the tables as they were before these rows.
	if err := RebuildSilver(ctx, s.st.c); err != nil {
		s.t.Fatal(err)
	}
}

// call adds a tool call that ran for person p: Claude Code's decision, then its result.
func (s *seed) call(p int, at time.Time, id, tool string, ok bool) {
	q := s.prompt(p, "1")
	s.otel(p, at, "tool_decision", q, "tool_name", tool, "tool_use_id", id, "decision", "accept", "source", "config")
	s.otel(p, at.Add(time.Second), "tool_result", q, "tool_name", tool, "tool_use_id", id, "success", fmt.Sprint(ok))
}

// seedWeek adds to seedFleet's week what only a weekly view can see, each part for one claim:
//
//   - persons 8 to 10, in the small cohort, one a day from Tuesday to Thursday. With 6 and 7 that
//     cohort has five people in the week and never more than two on a day: named weekly, "other"
//     daily.
//   - person 8 on a Claude Code release nobody else runs, which is "other releases".
//   - person 9's MCP tool failing three times in a row, a loop on a tool one person calls, which
//     is "other tools"; person 10's Bash failing three times and then passing, a loop on a tool
//     that is always named; and two failures in a row from person 8, which are not a loop.
//   - the week before, a session loading only v1 of the rules file, when v1 was the newest anyone
//     had loaded: current that week, stale in the next.
func seedWeek(s *seed) {
	s.release = map[int]string{8: "2.1.282"}
	for p := 8; p <= 10; p++ {
		start := s.at(time.Duration(p-7)*24*time.Hour + time.Duration(p)*time.Minute)
		q := s.prompt(p, "1")
		s.otel(p, start, "user_prompt", q, "prompt_length", "10")
		s.request(p, start.Add(5*time.Second), q, "m1", 100, 800, 100, "0.01")
	}
	tu := func(n string) string { return "tu-" + s.run + "-" + n }
	day := func(p int) time.Time { return s.at(time.Duration(p-7)*24*time.Hour + 30*time.Minute) }
	s.call(8, day(8), tu("r1"), "Read", false)
	s.call(8, day(8).Add(time.Minute), tu("r2"), "Read", false)
	s.call(8, day(8).Add(2*time.Minute), tu("r3"), "Read", true)
	for i := range 3 {
		s.call(9, day(9).Add(time.Duration(i)*time.Minute), tu(fmt.Sprint("m", i)), "mcp__"+s.run+"-db__query", false)
		s.call(10, day(10).Add(time.Duration(i)*time.Minute), tu(fmt.Sprint("b", i)), "Bash", false)
	}
	s.call(10, day(10).Add(3*time.Minute), tu("b3"), "Bash", true)

	s.hook(11, s.at(-7*24*time.Hour), "InstructionsLoaded", "", "instructions_name", "acme-"+s.run+"-rules-v1.md",
		"instructions_file", "rule", "load_reason", "session_start", "memory_type", "Project")
	s.write()
}

// seedFleet writes the whole scenario. Each comment says what the scenario is there to prove.
//
// Each test seeds its own week, eight or nine weeks back, inside the 90-day retention and clear of
// the other test's rows.
func seedFleet(t *testing.T, st *Store, weeksBack int) *seed {
	s := newSeed(t, st, weeksBack)
	for p := 1; p <= 7; p++ {
		start := s.at(time.Duration(p) * time.Minute)
		q := s.prompt(p, "1")
		s.otel(p, start, "user_prompt", q, "prompt_length", "10")
		s.hook(p, start.Add(-2*time.Second), "UserPromptSubmit", q, "prompt_length", "10", "permission_mode", "default")
		s.request(p, start.Add(5*time.Second), q, "m1", 100, 800, 100, "0.01")

		// A home-grown command five people use is named. One two people use is "other".
		if p <= 5 {
			s.hook(p, start, "UserPromptExpansion", q, "command_name", s.run+"-shared", "command_source", "userSettings")
		} else {
			s.hook(p, start, "UserPromptExpansion", q, "command_name", s.run+"-rare", "command_source", "userSettings")
		}
	}

	// Context per turn: the first main-thread request, and the largest.
	s.request(1, s.at(time.Minute+20*time.Second), s.prompt(1, "1"), "m1", 200, 2600, 200, "0.02")

	// The organization's own names are shown whoever uses them, even one person.
	s.hook(6, s.at(6*time.Minute), "UserPromptExpansion", s.prompt(6, "1"),
		"command_name", "acme-"+s.run+"-tool", "command_source", "projectSettings")
	s.otel(1, s.at(90*time.Second), "tool_decision", s.prompt(1, "1"),
		"tool_name", "mcp__acme-"+s.run+"-jira__search", "tool_use_id", "tu-"+s.run+"-mcp",
		"decision", "accept", "source", "config")
	// So are Claude Code's own subagents.
	s.hook(7, s.at(7*time.Minute), "SubagentStart", s.prompt(7, "1"), "agent_type", "Explore", "agent_id", "a-"+s.run)

	// Stale instructions: one person still on v1 of a file the other four load as v2.
	s.hook(1, s.at(time.Minute), "InstructionsLoaded", "", "instructions_name", "acme-"+s.run+"-rules-v1.md",
		"instructions_file", "rule", "load_reason", "session_start", "memory_type", "Project")
	s.hook(1, s.at(time.Minute), "InstructionsLoaded", "", "instructions_file", "CLAUDE.md",
		"load_reason", "session_start", "memory_type", "User")
	for p := 2; p <= 5; p++ {
		s.hook(p, s.at(time.Duration(p)*time.Minute), "InstructionsLoaded", "", "instructions_name", "acme-"+s.run+"-rules-v2.md",
			"instructions_file", "rule", "load_reason", "session_start", "memory_type", "Project")
	}

	// A permission wait, both ends on the laptop's clock. The hook row arrives stamped 2 s early,
	// as the collector's clock does, and must not be the start. A config decision for the same
	// tool in between is not a person's answer and must be skipped.
	t1 := s.at(30 * time.Minute)
	s.otel(1, t1, "hook_execution_start", s.prompt(1, "1"), "hook_event", "PermissionRequest",
		"hook_name", "PermissionRequest:Bash", "num_hooks", "1")
	s.hook(1, t1.Add(-2*time.Second), "PermissionRequest", s.prompt(1, "1"), "tool_name", "Bash",
		"permission_mode", "default", "agent_type", "Explore")
	s.otel(1, t1.Add(time.Second), "tool_decision", s.prompt(1, "1"), "tool_name", "Bash",
		"tool_use_id", "tu-"+s.run+"-auto", "decision", "accept", "source", "config")
	s.otel(1, t1.Add(10*time.Second), "tool_decision", s.prompt(1, "1"), "tool_name", "Bash",
		"tool_use_id", "tu-"+s.run+"-perm1", "decision", "accept", "source", "user_temporary")

	// A session that sent no hook_execution_start at all falls back to the hook row's time and says so.
	t2 := s.at(40 * time.Minute)
	s.hook(6, t2, "PermissionRequest", s.prompt(6, "1"), "tool_name", "Edit", "permission_mode", "default")
	s.otel(6, t2.Add(4*time.Second), "tool_decision", s.prompt(6, "1"), "tool_name", "Edit",
		"tool_use_id", "tu-"+s.run+"-perm6", "decision", "reject", "source", "user_reject")

	// Edits in the big cohort: three proposed, one rejected.
	s.otel(4, s.at(45*time.Minute), "tool_decision", s.prompt(4, "1"), "tool_name", "Edit",
		"tool_use_id", "tu-"+s.run+"-e1", "decision", "reject", "source", "user_reject")
	s.otel(4, s.at(46*time.Minute), "tool_decision", s.prompt(4, "1"), "tool_name", "Edit",
		"tool_use_id", "tu-"+s.run+"-e2", "decision", "accept", "source", "config")
	s.otel(5, s.at(47*time.Minute), "tool_decision", s.prompt(5, "1"), "tool_name", "Write",
		"tool_use_id", "tu-"+s.run+"-e3", "decision", "accept", "source", "config")

	// A model switch. Its time comes from hook_execution_start, and its measured cost from the
	// next main-thread request on the new model. The request on the old model in between is not it.
	t3 := s.at(50 * time.Minute)
	s.hook(2, t3.Add(-2*time.Second), "PreModelSwitch", s.prompt(2, "sw"), "from_model", "m1", "to_model", "m2",
		"context_tokens", "1000", "prompt_cache_warm", "true", "cache_ttl", "1h", "estimated_cache_write_usd", "0.4")
	s.otel(2, t3, "hook_execution_start", s.prompt(2, "sw"), "hook_event", "PreModelSwitch",
		"hook_name", "PreModelSwitch:m2", "num_hooks", "1")
	s.request(2, t3.Add(-time.Second), s.prompt(2, "1"), "m1", 10, 900, 90, "0.99")
	s.request(2, t3.Add(30*time.Second), s.prompt(2, "2"), "m2", 10, 400, 600, "0.05")

	// The same from PostModelSwitch, which the bundle hooks since ADR-0039, as Claude Code 2.1.289
	// sends it: no prompt id on either side, and a dated to_model where the hook's name has the
	// canonical one. The next request names the model without the date too: which spelling a real
	// api_request uses beside a dated to_model has not been observed, so both are normalized. Kept
	// out of the big cohort, whose gold row counts switches, and out of person 6's session, which
	// must send no hook_execution_start.
	t4 := s.at(60 * time.Minute)
	s.hook(7, t4.Add(-2*time.Second), "PostModelSwitch", "", "from_model", "m1", "to_model", "m3-20251001",
		"model_switch_source", "picker", "context_tokens", "2000", "estimated_cache_write_usd", "0.7")
	s.otel(7, t4, "hook_execution_start", "", "hook_event", "PostModelSwitch",
		"hook_name", "PostModelSwitch:m3", "num_hooks", "1")
	s.request(7, t4.Add(20*time.Second), s.prompt(7, "2"), "m3", 10, 0, 1900, "0.30")
	// A resumed session restoring its model is not a switch anyone asked for.
	s.hook(6, s.at(61*time.Minute), "PostModelSwitch", "", "from_model", "m1", "to_model", "m3-20251001",
		"model_switch_source", "resume")

	// Compactions: one with OTel's token counts, and one from a session that sent only the hook.
	s.otel(3, s.at(55*time.Minute), "compaction", s.prompt(3, "1"), "trigger", "auto",
		"pre_tokens", "5000", "post_tokens", "1000", "duration_ms", "9000")
	s.hook(7, s.at(55*time.Minute), "PreCompact", s.prompt(7, "1"), "compaction_reason", "manual")

	s.write()
	return s
}

// setViewSettings writes settings for the test and restores the effective values afterwards.
func setViewSettings(t *testing.T, st *Store, vs ViewSettings) {
	t.Helper()
	ctx := context.Background()
	org, size, err := st.EffectiveViewSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		prev := fmt.Sprint(size)
		if err := st.ApplyViewSettings(context.Background(), ViewSettings{OrgArtifacts: &org, MinGroupSize: &prev}); err != nil {
			t.Errorf("could not restore the view settings: %v", err)
		}
	})
	if err := st.ApplyViewSettings(ctx, vs); err != nil {
		t.Fatal(err)
	}
}

func strPtr(s string) *string { return &s }

// rowsOf runs a query and returns each row with every value as text.
func rowsOf(t *testing.T, st *Store, q string) []map[string]string {
	t.Helper()
	out := query(t, st, q+" FORMAT JSONEachRow")
	var rows []map[string]string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("decoding %q: %v", line, err)
		}
		row := map[string]string{}
		for k, v := range raw {
			if v == nil {
				row[k] = "NULL"
			} else {
				row[k] = fmt.Sprint(v)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func expect(t *testing.T, what string, row map[string]string, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if row[k] != v {
			t.Errorf("%s: %s = %q, want %q (row %v)", what, k, row[k], v, row)
		}
	}
}

func oneRow(t *testing.T, st *Store, what, q string) map[string]string {
	t.Helper()
	rows := rowsOf(t, st, q)
	if len(rows) != 1 {
		t.Fatalf("%s: got %d rows, want 1: %v", what, len(rows), rows)
	}
	return rows[0]
}

// The silver views join across two clocks and two paths. Each value here is one a design error
// would change: a wait measured from the hook row is 2 s longer, a switch timed by it is 2 s
// earlier, and a turn's peak is not its first request.
func TestCollectorSilverViews(t *testing.T) {
	st := testStore(t)
	setViewSettings(t, st, ViewSettings{OrgArtifacts: strPtr(testOrgPattern), MinGroupSize: strPtr("")})
	s := seedFleet(t, st, 8)

	sess := oneRow(t, st, "session", fmt.Sprintf(
		"SELECT pseudonym, cohort, clock, started_at FROM cardo.silver_session WHERE session_id = '%s'", s.session(1)))
	expect(t, "silver_session", sess, map[string]string{
		"pseudonym": s.person(1), "cohort": s.cohort(1), "clock": "client",
		// The laptop's first event, not the hook rows stamped earlier by the collector's clock.
		"started_at": stamp(s.at(time.Minute)),
	})

	turn := oneRow(t, st, "turn", fmt.Sprintf(
		"SELECT pseudonym, permission_mode, first_context_tokens, peak_context_tokens, main_thread_requests FROM cardo.silver_turn WHERE prompt_id = '%s'",
		s.prompt(1, "1")))
	expect(t, "silver_turn", turn, map[string]string{
		"pseudonym": s.person(1), "permission_mode": "default",
		"first_context_tokens": "1000", "peak_context_tokens": "3000", "main_thread_requests": "2",
	})

	waits := rowsOf(t, st, fmt.Sprintf(
		"SELECT session_id, tool_name, clock, answered, decision_source, wait_ms, agent_type FROM cardo.silver_policy_decision WHERE startsWith(session_id, 's-%s-') ORDER BY session_id",
		s.run))
	if len(waits) != 2 {
		t.Fatalf("silver_policy_decision: got %d rows, want 2: %v", len(waits), waits)
	}
	expect(t, "wait on the laptop's clock", waits[0], map[string]string{
		"tool_name": "Bash", "clock": "client", "answered": "1", "decision_source": "user_temporary",
		"wait_ms": "10000", "agent_type": "Explore",
	})
	expect(t, "wait from the fallback", waits[1], map[string]string{
		"tool_name": "Edit", "clock": "two_clocks", "decision_source": "user_reject", "wait_ms": "4000",
	})

	sw := oneRow(t, st, "switch", fmt.Sprintf(
		"SELECT clock, at, to_model, estimated_cache_write_usd, next_request_cache_creation_tokens, next_request_cost_usd FROM cardo.silver_context_event WHERE kind = 'model_switch' AND session_id = '%s'",
		s.session(2)))
	expect(t, "silver_context_event switch", sw, map[string]string{
		"clock": "client", "at": stamp(s.at(50 * time.Minute)), "to_model": "m2",
		"estimated_cache_write_usd": "0.4", "next_request_cache_creation_tokens": "600", "next_request_cost_usd": "0.05",
	})
	post := oneRow(t, st, "PostModelSwitch", fmt.Sprintf(
		"SELECT clock, at, to_model, switch_source, estimated_cache_write_usd, next_request_cache_creation_tokens, next_request_cost_usd FROM cardo.silver_context_event WHERE kind = 'model_switch' AND session_id = '%s'",
		s.session(7)))
	expect(t, "silver_context_event switch from PostModelSwitch", post, map[string]string{
		"clock": "client", "at": stamp(s.at(60 * time.Minute)), "to_model": "m3-20251001", "switch_source": "picker",
		"estimated_cache_write_usd": "0.7", "next_request_cache_creation_tokens": "1900", "next_request_cost_usd": "0.3",
	})
	if r := rowsOf(t, st, fmt.Sprintf(
		"SELECT at FROM cardo.silver_context_event WHERE kind = 'model_switch' AND session_id = '%s'", s.session(6))); len(r) != 0 {
		t.Errorf("silver_context_event counts a model restored on resume as a switch: %v", r)
	}
	compactions := rowsOf(t, st, fmt.Sprintf(
		"SELECT session_id, clock, compaction_reason, tokens_before, tokens_after FROM cardo.silver_context_event WHERE kind = 'compaction' AND startsWith(session_id, 's-%s-') ORDER BY session_id",
		s.run))
	if len(compactions) != 2 {
		t.Fatalf("compactions: got %d rows, want 2: %v", len(compactions), compactions)
	}
	expect(t, "compaction from OTel", compactions[0], map[string]string{
		"clock": "client", "compaction_reason": "auto", "tokens_before": "5000", "tokens_after": "1000",
	})
	expect(t, "compaction from the hook alone", compactions[1], map[string]string{
		"clock": "collector", "compaction_reason": "manual", "tokens_before": "0",
	})

	arts := rowsOf(t, st, fmt.Sprintf(
		"SELECT kind, name, is_org, is_builtin, count() AS n FROM cardo.silver_artifact_load WHERE startsWith(session_id, 's-%s-') GROUP BY kind, name, is_org, is_builtin ORDER BY kind, name",
		s.run))
	got := map[string]string{}
	for _, a := range arts {
		got[a["kind"]+"/"+a["name"]] = a["is_org"] + a["is_builtin"] + "x" + a["n"]
	}
	for k, v := range map[string]string{
		"instructions/": "00x1",
		"instructions/acme-" + s.run + "-rules-v1.md": "10x1",
		"instructions/acme-" + s.run + "-rules-v2.md": "10x4",
		"mcp/acme-" + s.run + "-jira":                 "10x1",
		"skill/" + s.run + "-shared":                  "00x5",
		"skill/" + s.run + "-rare":                    "00x2",
		"skill/acme-" + s.run + "-tool":               "10x1",
		"subagent/Explore":                            "01x1",
	} {
		if got[k] != v {
			t.Errorf("silver_artifact_load %s: is_org, is_builtin x uses = %q, want %q", k, got[k], v)
		}
	}
}

// ADR-0029, as a dashboard would meet it: names and cohorts under the minimum group size are
// "other", the organization's names and fleet totals are not, and the minimum can be raised but
// not lowered.
func TestCollectorGoldAppliesTheMinimumGroupSize(t *testing.T) {
	st := testStore(t)
	setViewSettings(t, st, ViewSettings{OrgArtifacts: strPtr(testOrgPattern), MinGroupSize: strPtr("")})
	s := seedFleet(t, st, 9)
	week := s.day.Format("2006-01-02")

	usage := func() map[string]map[string]string {
		out := map[string]map[string]string{}
		for _, r := range rowsOf(t, st, fmt.Sprintf(
			"SELECT kind, artifact, file_kind, people, sessions, is_org FROM cardo.gold_artifact_usage WHERE week = '%s'", week)) {
			out[r["kind"]+"/"+r["artifact"]+"/"+r["file_kind"]] = r
		}
		return out
	}
	u := usage()
	for key, want := range map[string]map[string]string{
		"skill/" + s.run + "-shared/":                      {"people": "5"},
		"skill/acme-" + s.run + "-tool/":                   {"people": "1", "is_org": "1"},
		"mcp/acme-" + s.run + "-jira/":                     {"people": "1", "is_org": "1"},
		"subagent/Explore/":                                {"people": "1"},
		"instructions/acme-" + s.run + "-rules-v2.md/rule": {"people": "4", "is_org": "1"},
		"skill/other/":                                     {"people": "2"},
		"instructions//CLAUDE.md":                          {"people": "1"},
	} {
		if u[key] == nil {
			t.Errorf("gold_artifact_usage has no row %q; rows: %v", key, u)
			continue
		}
		expect(t, "gold_artifact_usage "+key, u[key], want)
	}
	if u["skill/"+s.run+"-rare/"] != nil {
		t.Errorf("INV-3 / ADR-0029: a command two people use is named on a dashboard: %v", u["skill/"+s.run+"-rare/"])
	}

	versions := rowsOf(t, st, fmt.Sprintf(
		"SELECT version, is_current, current_version, people FROM cardo.gold_instructions_versions WHERE week = '%s' AND family = 'acme-%s-rules' ORDER BY version",
		week, s.run))
	if len(versions) != 2 {
		t.Fatalf("gold_instructions_versions: got %d rows, want 2: %v", len(versions), versions)
	}
	expect(t, "stale version", versions[0], map[string]string{"version": "1", "is_current": "0", "current_version": "2", "people": "1"})
	expect(t, "current version", versions[1], map[string]string{"version": "2", "is_current": "1", "people": "4"})

	cohorts := func() map[string]map[string]string {
		out := map[string]map[string]string{}
		for _, r := range rowsOf(t, st, fmt.Sprintf("SELECT * FROM cardo.gold_friction_daily WHERE day = '%s'", week)) {
			out[r["cohort"]] = r
		}
		return out
	}
	f := cohorts()
	if f[s.run+"-small"] != nil {
		t.Errorf("INV-3 / ADR-0029: a cohort of two is named: %v", f[s.run+"-small"])
	}
	if f[s.run+"-big"] == nil || f["other"] == nil {
		t.Fatalf("gold_friction_daily: want the big cohort and \"other\", got %v", f)
	}
	expect(t, "friction, big cohort", f[s.run+"-big"], map[string]string{
		"active_people": "5", "sessions": "5", "prompts": "5",
		"edit_proposals": "3", "edit_rejections": "1", "edit_rejection_rate": "0.3333",
		"permission_prompts": "1", "permission_answered": "1", "permission_prompts_two_clocks": "0",
		"permission_wait_s_p50": "10", "compactions": "1", "compactions_per_session": "0.2",
	})
	expect(t, "friction, other", f["other"], map[string]string{
		"active_people": "2", "permission_prompts_two_clocks": "1", "permission_wait_s_p50": "4",
	})

	ctxRow := oneRow(t, st, "context", fmt.Sprintf(
		"SELECT * FROM cardo.gold_context_daily WHERE day = '%s' AND cohort = '%s-big'", week, s.run))
	expect(t, "gold_context_daily", ctxRow, map[string]string{
		"model_switches": "1", "model_switches_measured": "1", "switch_cache_write_tokens": "600",
		"switch_next_request_cost_usd": "0.05", "model_switches_estimated": "1", "switch_estimated_cache_write_usd": "0.4",
		"session_start_context_p50": "1000", "turn_peak_context_max": "3000",
	})

	// The weekly views (018). The daily views above saw seedFleet's Monday alone; the rest of the
	// week arrives now.
	seedWeek(s)
	prev := s.day.AddDate(0, 0, -7).Format("2006-01-02")

	spend := func() map[string]map[string]string {
		out := map[string]map[string]string{}
		for _, r := range rowsOf(t, st, fmt.Sprintf("SELECT cohort, sessions, cost_usd FROM cardo.gold_spend_weekly WHERE week = '%s'", week)) {
			out[r["cohort"]] = r
		}
		return out
	}
	w := spend()
	expect(t, "weekly spend, big cohort", w[s.run+"-big"], map[string]string{"sessions": "5", "cost_usd": "1.11"})
	// Two people on Monday and one on each of three other days: never five on a day, five in the week.
	expect(t, "weekly spend, small cohort", w[s.run+"-small"], map[string]string{"sessions": "5", "cost_usd": "0.35"})
	if r := rowsOf(t, st, fmt.Sprintf("SELECT cohort FROM cardo.gold_friction_daily WHERE day > '%s' AND day < toMonday(toDate('%s') + 7) AND cohort = '%s-small'", week, week, s.run)); len(r) != 0 {
		t.Errorf("a cohort of one a day is named on a daily view: %v", r)
	}
	ctxWeek := oneRow(t, st, "weekly context", fmt.Sprintf(
		"SELECT active_people, sessions, model_switches FROM cardo.gold_context_weekly WHERE week = '%s' AND cohort = '%s-small'", week, s.run))
	expect(t, "gold_context_weekly, small cohort", ctxWeek, map[string]string{"active_people": "5", "sessions": "5", "model_switches": "1"})

	models := map[string]map[string]string{}
	for _, r := range rowsOf(t, st, fmt.Sprintf("SELECT model, people, cost_usd FROM cardo.gold_model_spend_weekly WHERE week = '%s'", week)) {
		models[r["model"]] = r
	}
	// m2 and m3 each had one person: what they cost is that person's spend.
	expect(t, "model spend, m1", models["m1"], map[string]string{"people": "10", "cost_usd": "1.11"})
	expect(t, "model spend, other models", models["other models"], map[string]string{"people": "2", "cost_usd": "0.35"})
	if models["m2"] != nil || models["m3"] != nil {
		t.Errorf("INV-3 / ADR-0029: a model one person used is named: %v", models)
	}

	releases := map[string]map[string]string{}
	for _, r := range rowsOf(t, st, fmt.Sprintf("SELECT app_version, people, requests, cache_read_share FROM cardo.gold_release_cache_weekly WHERE week = '%s'", week)) {
		releases[r["app_version"]] = r
	}
	expect(t, "release 2.1.281", releases["2.1.281"], map[string]string{"people": "9"})
	// 800 of 1,000 input tokens read from the cache.
	expect(t, "other releases", releases["other releases"], map[string]string{"people": "1", "requests": "1", "cache_read_share": "0.8"})
	if releases["2.1.282"] != nil {
		t.Errorf("INV-3 / ADR-0029: a release one person runs is named: %v", releases["2.1.282"])
	}

	loops := map[string]map[string]string{}
	for _, r := range rowsOf(t, st, fmt.Sprintf("SELECT tool_name, calls, loops, failed_in_loops, sessions_with_loops FROM cardo.gold_tool_loops_weekly WHERE week = '%s'", week)) {
		loops[r["tool_name"]] = r
	}
	expect(t, "loops, Bash", loops["Bash"], map[string]string{"calls": "4", "loops": "1", "failed_in_loops": "3", "sessions_with_loops": "1"})
	expect(t, "loops, other tools", loops["other tools"], map[string]string{"calls": "3", "loops": "1", "failed_in_loops": "3"})
	expect(t, "two failures in a row are not a loop", loops["Read"], map[string]string{"calls": "3", "loops": "0"})
	if loops["mcp__"+s.run+"-db__query"] != nil {
		t.Errorf("INV-3 / ADR-0029: an MCP tool one person calls is named: %v", loops)
	}

	setup := oneRow(t, st, "setup context", fmt.Sprintf(
		"SELECT sessions_with_requests, session_start_context_p50 FROM cardo.gold_setup_context_weekly WHERE week = '%s'", week))
	expect(t, "gold_setup_context_weekly", setup, map[string]string{"sessions_with_requests": "10", "session_start_context_p50": "1000"})

	// Current is judged by the end of each week: v1 was current the week before v2 appeared.
	byWeek := map[string]map[string]string{}
	for _, r := range rowsOf(t, st, fmt.Sprintf(
		"SELECT toString(week) AS week, version, is_current, current_version, toString(first_week) AS first_week FROM cardo.gold_instructions_versions WHERE family = 'acme-%s-rules'", s.run)) {
		byWeek[r["week"]+"/v"+r["version"]] = r
	}
	expect(t, "v1 the week before v2", byWeek[prev+"/v1"], map[string]string{"is_current": "1", "current_version": "1", "first_week": prev})
	expect(t, "v1 once v2 is loaded", byWeek[week+"/v1"], map[string]string{"is_current": "0", "current_version": "2", "first_week": prev})
	expect(t, "v2", byWeek[week+"/v2"], map[string]string{"is_current": "1", "first_week": week})

	team := oneRow(t, st, "instructions by team", fmt.Sprintf(
		"SELECT sessions, sessions_current, current_share FROM cardo.gold_instructions_by_team_weekly WHERE week = '%s' AND cohort = '%s-big' AND family = 'acme-%s-rules'", week, s.run, s.run))
	expect(t, "gold_instructions_by_team_weekly, big cohort", team, map[string]string{"sessions": "5", "sessions_current": "4", "current_share": "0.8"})

	spread := map[string]map[string]string{}
	for _, r := range rowsOf(t, st, fmt.Sprintf(
		"SELECT kind, artifact, teams, toString(first_week) AS first_week FROM cardo.gold_artifact_usage WHERE week = '%s' AND kind = 'skill'", week)) {
		spread[r["artifact"]] = r
	}
	expect(t, "teams and first week, shared command", spread[s.run+"-shared"], map[string]string{"teams": "1", "first_week": week})
	expect(t, "first week of \"other\"", spread["other"], map[string]string{"first_week": "NULL"})

	// Raised to six: the command five people use, and the cohort of five, become "other".
	if err := st.ApplyViewSettings(context.Background(), ViewSettings{MinGroupSize: strPtr("6")}); err != nil {
		t.Fatal(err)
	}
	if u := usage(); u["skill/"+s.run+"-shared/"] != nil {
		t.Errorf("with a minimum of 6, a command five people use is still named")
	}
	if f := cohorts(); f[s.run+"-big"] != nil {
		t.Errorf("with a minimum of 6, a cohort of five is still named")
	}
	if w := spend(); w[s.run+"-small"] != nil || w[s.run+"-big"] != nil {
		t.Errorf("with a minimum of 6, a cohort of five is still named weekly: %v", w)
	}

	// A value below the floor, written straight into the table as nothing but a hand could, is
	// read as the floor.
	if err := st.c.Exec(context.Background(), "INSERT INTO cardo.settings (key, value, version) "+
		"SELECT 'min_group_size', '2', max(version) + 1 FROM cardo.settings"); err != nil {
		t.Fatal(err)
	}
	if _, size, _ := st.EffectiveViewSettings(context.Background()); size != MinGroupSizeFloor {
		t.Errorf("a stored minimum group size of 2 reads as %d, want the floor %d", size, MinGroupSizeFloor)
	}
	if u := usage(); u["skill/"+s.run+"-rare/"] != nil {
		t.Errorf("a stored minimum of 2 named a command two people use")
	}
}

// Service runs are not people (ADR-0036). A nightly job with a pseudonym of its own runs in a team
// beside four engineers. Counted as a person, it would make the team five, so the team would be
// named, and its model, its release, its command and the newer instructions file only it loaded
// would each show. Left out, as every weekly view and both artifact views must leave it, the week
// holds four people and nothing of the job.
func TestWeeklyViewsLeaveServiceRunsOut(t *testing.T) {
	st := testStore(t)
	setViewSettings(t, st, ViewSettings{OrgArtifacts: strPtr(testOrgPattern), MinGroupSize: strPtr("")})
	s := newSeed(t, st, 11)
	s.release = map[int]string{5: "2.1.299"}
	s.workflow = map[int]string{5: "nightly-" + s.run}
	week := s.day.Format("2006-01-02")
	for p := 1; p <= 5; p++ {
		start := s.at(time.Duration(p) * time.Minute)
		q := s.prompt(p, "1")
		model, cost := "m1", "0.01"
		file, command := "acme-"+s.run+"-rules-v2.md", "acme-"+s.run+"-tool"
		if p == 5 {
			model, cost = "m9-"+s.run, "5"
			file, command = "acme-"+s.run+"-rules-v3.md", s.run+"-ci"
		}
		s.otel(p, start, "user_prompt", q, "prompt_length", "10")
		s.request(p, start.Add(5*time.Second), q, model, 100, 800, 100, cost)
		s.hook(p, start, "InstructionsLoaded", "", "instructions_name", file,
			"instructions_file", "rule", "load_reason", "session_start", "memory_type", "Project")
		s.hook(p, start, "UserPromptExpansion", q, "command_name", command, "command_source", "projectSettings")
	}
	s.call(1, s.at(10*time.Minute), "tu-"+s.run+"-ok", "Bash", true)
	for i := range 3 {
		s.call(5, s.at(20*time.Minute+time.Duration(i)*time.Minute), fmt.Sprint("tu-", s.run, "-ci", i), "Bash", false)
	}
	s.write()

	job := oneRow(t, st, "the job's session", fmt.Sprintf(
		"SELECT actor_kind, workflow FROM cardo.silver_session WHERE session_id = '%s'", s.session(5)))
	if job["actor_kind"] != "service" {
		t.Fatalf("the seeded job is not a service run, so this test proves nothing: %v", job)
	}
	if r := rowsOf(t, st, fmt.Sprintf(
		"SELECT cohort FROM cardo.silver_session_cohort_week WHERE session_id = '%s'", s.session(5))); len(r) != 0 {
		t.Errorf("silver_session_cohort_week puts the job in a team, so every weekly view reads it: %v", r)
	}

	// Each weekly view holds one row: four people are fewer than five, so the team, the model and
	// the release are all "other", and the job's $5, its model and its release are in none of them.
	only := func(what, q string, want map[string]string) {
		t.Helper()
		rows := rowsOf(t, st, fmt.Sprintf(q, week))
		if len(rows) != 1 {
			t.Errorf("%s: got %d rows, want 1: %v", what, len(rows), rows)
			return
		}
		expect(t, what, rows[0], want)
	}
	only("gold_spend_weekly", "SELECT cohort, sessions, cost_usd FROM cardo.gold_spend_weekly WHERE week = '%s'",
		map[string]string{"cohort": "other", "sessions": "4", "cost_usd": "0.04"})
	only("gold_model_spend_weekly", "SELECT model, people, cost_usd FROM cardo.gold_model_spend_weekly WHERE week = '%s'",
		map[string]string{"model": "other models", "people": "4", "cost_usd": "0.04"})
	only("gold_release_cache_weekly", "SELECT app_version, people, requests FROM cardo.gold_release_cache_weekly WHERE week = '%s'",
		map[string]string{"app_version": "other releases", "people": "4", "requests": "4"})
	only("gold_context_weekly", "SELECT cohort, active_people, sessions FROM cardo.gold_context_weekly WHERE week = '%s'",
		map[string]string{"cohort": "other", "active_people": "4", "sessions": "4"})
	only("gold_setup_context_weekly", "SELECT sessions_with_requests FROM cardo.gold_setup_context_weekly WHERE week = '%s'",
		map[string]string{"sessions_with_requests": "4"})
	only("gold_tool_loops_weekly", "SELECT tool_name, calls, loops FROM cardo.gold_tool_loops_weekly WHERE week = '%s'",
		map[string]string{"tool_name": "Bash", "calls": "1", "loops": "0"})
	only("gold_instructions_by_team_weekly", "SELECT cohort, sessions, sessions_current FROM cardo.gold_instructions_by_team_weekly WHERE week = '%s'",
		map[string]string{"cohort": "other", "sessions": "4", "sessions_current": "4"})
	// v3 is the organization's file, so even one person loading it would show it, and make v2 stale.
	only("gold_instructions_versions", "SELECT version, is_current, people FROM cardo.gold_instructions_versions WHERE week = '%s'",
		map[string]string{"version": "2", "is_current": "1", "people": "4"})

	usage := map[string]map[string]string{}
	for _, r := range rowsOf(t, st, fmt.Sprintf(
		"SELECT kind, artifact, file_kind, people, active_people FROM cardo.gold_artifact_usage WHERE week = '%s'", week)) {
		usage[r["kind"]+"/"+r["artifact"]+"/"+r["file_kind"]] = r
	}
	if len(usage) != 2 {
		t.Errorf("gold_artifact_usage: want the organization's command and its v2 file and nothing of the job, got %v", usage)
	}
	expect(t, "the organization's command", usage["skill/acme-"+s.run+"-tool/"], map[string]string{"people": "4", "active_people": "4"})
	expect(t, "the organization's v2 file", usage["instructions/acme-"+s.run+"-rules-v2.md/rule"], map[string]string{"people": "4", "active_people": "4"})
}

// migrate refuses a group size below the floor before writing anything.
func TestViewSettingsRefuseToLowerTheFloor(t *testing.T) {
	for _, v := range []string{"0", "1", "4", "-5", "five", "4.9"} {
		if err := (ViewSettings{MinGroupSize: strPtr(v)}).Validate(); err == nil {
			t.Errorf("CARDO_MIN_GROUP_SIZE=%q was accepted", v)
		}
	}
	for _, v := range []string{"", "5", "10", " 12 "} {
		if err := (ViewSettings{MinGroupSize: strPtr(v)}).Validate(); err != nil {
			t.Errorf("CARDO_MIN_GROUP_SIZE=%q was refused: %v", v, err)
		}
	}
	if err := (ViewSettings{OrgArtifacts: strPtr("^acme-(")}).Validate(); err == nil {
		t.Error("an invalid CARDO_ORG_ARTIFACTS was accepted")
	}
}

// The last write wins, whatever the server's clock does, and a pattern full of the characters SQL
// quoting cares about comes back exactly as written.
func TestViewSettingsLastWriteWins(t *testing.T) {
	st := testStore(t)
	awkward := `^(acme|it's)-\d+"x`
	setViewSettings(t, st, ViewSettings{OrgArtifacts: strPtr("^first-"), MinGroupSize: strPtr("9")})
	for i := 0; i < 20; i++ {
		size := fmt.Sprint(5 + i)
		if err := st.ApplyViewSettings(context.Background(), ViewSettings{OrgArtifacts: &awkward, MinGroupSize: &size}); err != nil {
			t.Fatal(err)
		}
	}
	org, size, err := st.EffectiveViewSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if org != awkward || size != 24 {
		t.Errorf("after twenty writes in a row the views read %q and %d, want %q and 24", org, size, awkward)
	}
}
