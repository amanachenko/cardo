package clickhouse

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The daily-aggregate family must see both ingestion paths, and never count a day twice. The
// Admin API row wins the person's day; the sessions path fills only what the ledger does not
// have. Lines, commits, pull requests and edit decisions stay zero on derived rows -- only the
// ledger knows them -- and edit acceptance reads the ledger alone. A service run is not a person,
// so neither its session nor its spend is in the derived rows.
//
// The day is one no other data uses (the day after the newest session in the store), so the gold
// views' day-level sums are exactly this scenario's.
func TestGoldAggregatesReadBothPaths(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	run := fmt.Sprintf("g%d", time.Now().UnixNano()%1_000_000_000)
	day := testDay(1).Format("2006-01-02")
	p1, p2 := "tp1-"+run, "tp2-"+run

	t.Cleanup(func() {
		st.c.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE startsWith(LogAttributes['session.id'], 's-%s-')", logsTable, run))
		st.c.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE pseudonym IN ('%s', '%s', 'pa-%s')", BronzeTable, p1, p2, run))
	})

	// The sessions path. Person tp1 works, spends 0.25, and has one edit accepted and one
	// rejected. Person tp2 spends 9.99 -- and also has a ledger day, which must win outright.
	service := map[string]bool{}
	otel := func(person, session, event, prompt string, kv ...string) {
		lg := "{'session.id': '" + session + "', 'prompt.id': '" + prompt + "'"
		for i := 0; i+1 < len(kv); i += 2 {
			lg += ", '" + kv[i] + "': '" + kv[i+1] + "'"
		}
		lg += "}"
		res := "{'user.pseudonym': '" + person + "', 'cardo.cohort': 'g'}"
		if service[session] {
			res = "{'user.pseudonym': '" + person + "', 'cardo.cohort': 'g', 'cardo.actor': 'service', 'cardo.workflow': 'nightly'}"
		}
		q := fmt.Sprintf("INSERT INTO %s (Timestamp, EventName, LogAttributes, ResourceAttributes) "+
			"VALUES ('%s 10:00:00', '%s', %s, %s)", logsTable, day, event, lg, res)
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	s1, s2 := "s-"+run+"-1", "s-"+run+"-2"
	otel(p1, s1, "claude_code.user_prompt", "pr1", "prompt_length", "10")
	otel(p1, s1, "claude_code.api_request", "pr1", "model", "m1",
		"input_tokens", "100", "output_tokens", "20",
		"cache_read_tokens", "10", "cache_creation_tokens", "5", "cost_usd", "0.25")
	otel(p1, s1, "claude_code.tool_decision", "pr1", "tool_use_id", "tu1", "tool_name", "Edit",
		"decision", "accept", "source", "config")
	otel(p1, s1, "claude_code.tool_decision", "pr1", "tool_use_id", "tu2", "tool_name", "Edit",
		"decision", "reject", "source", "user_reject")
	otel(p2, s2, "claude_code.user_prompt", "pr2", "prompt_length", "10")
	otel(p2, s2, "claude_code.api_request", "pr2", "model", "m1",
		"input_tokens", "100", "output_tokens", "20",
		"cache_read_tokens", "10", "cache_creation_tokens", "5", "cost_usd", "9.99")

	// A CI job, run as a service: its session and its 5.00 are in no derived row.
	p3, s3 := "tp3-"+run, "s-"+run+"-3"
	service[s3] = true
	otel(p3, s3, "claude_code.user_prompt", "pr3", "prompt_length", "10")
	otel(p3, s3, "claude_code.api_request", "pr3", "model", "m1",
		"input_tokens", "100", "output_tokens", "20",
		"cache_read_tokens", "10", "cache_creation_tokens", "5", "cost_usd", "5.00")

	// The ledger path: one person only here (pa), one in both (tp2). The payload is the admin
	// record's: three sessions, seven lines, one commit, two edits accepted and one rejected.
	payload := `{"core_metrics": {"num_sessions": 3, "lines_of_code": {"added": 7, "removed": 2},` +
		` "commits_by_claude_code": 1, "pull_requests_by_claude_code": 0},` +
		` "tool_actions": {"edit_tool": {"accepted": 2, "rejected": 1}}}`
	for _, person := range []string{"pa-" + run, p2} {
		q := fmt.Sprintf("INSERT INTO %s (source, day, pseudonym, salt_version, tier, actor_type, "+
			"org_id, customer_type, terminal_type, payload, unknown_fields) VALUES "+
			"('acme', '%s', '%s', 'v1', 1, 'user_actor', '', '', '', '%s', [])", BronzeTable, day, person, payload)
		if err := st.c.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	// Silver is stored and rebuilt every 10 minutes (ADR-0052): rebuild now, or the derived
	// daily aggregates read the tables as they were before these rows.
	if err := RebuildSilver(ctx, st.c); err != nil {
		t.Fatal(err)
	}

	// The two sources, one row each per measure. tp2's sessions-path day is suppressed by their
	// ledger day, so the sessions source counts tp1 alone.
	got := rowsOf(t, st, fmt.Sprintf(
		"SELECT source, sum(sessions) AS sessions, sum(lines_added) AS lines, sum(commits) AS commits "+
			"FROM cardo.gold_fleet_adoption WHERE day = '%s' AND source IN ('acme', 'sessions') "+
			"GROUP BY source ORDER BY source", day))
	if len(got) != 2 {
		t.Fatalf("gold_fleet_adoption: got %d source rows, want 2 (acme and sessions): %v", len(got), got)
	}
	expect(t, "ledger rows", got[0], map[string]string{
		"source": "acme", "sessions": "6", "lines": "14", "commits": "2"})
	expect(t, "sessions-path rows", got[1], map[string]string{
		"source": "sessions", "sessions": "1", "lines": "0", "commits": "0"})

	// The money: tp2's 9.99 is suppressed by their ledger day, and the service run's 5.00 is not a
	// person's; tp1's 0.25 is all the sessions path adds, from one person.
	money := oneRow(t, st, "cost", fmt.Sprintf(
		"SELECT round(sum(cost_usd), 2) AS c, sum(active_actors) AS people FROM cardo.gold_cost_daily "+
			"WHERE day = '%s' AND source = 'sessions'", day))
	expect(t, "daily spend", money, map[string]string{"c": "0.25", "people": "1"})

	// Edit acceptance reads the ledger alone: tp1's two edit decisions on the sessions path are
	// not in it, and the ledger's are (two people, two accepted and one rejected each).
	acc := rowsOf(t, st, fmt.Sprintf(
		"SELECT source, accepted, rejected FROM cardo.gold_tool_acceptance "+
			"WHERE day = '%s' AND source IN ('acme', 'sessions') ORDER BY source", day))
	if len(acc) != 1 {
		t.Fatalf("gold_tool_acceptance: got %d source rows, want the ledger's alone: %v", len(acc), acc)
	}
	expect(t, "tool acceptance", acc[0], map[string]string{"source": "acme", "accepted": "4", "rejected": "2"})
	edits := oneRow(t, st, "derived edits", fmt.Sprintf(
		"SELECT sum(edit_accepted + edit_rejected + write_accepted + write_rejected) AS n "+
			"FROM cardo.silver_actor_day_both WHERE day = '%s' AND source = 'sessions'", day))
	expect(t, "edit decisions on derived rows", edits, map[string]string{"n": "0"})

	// The model mix too, by the model the request carried.
	mix := oneRow(t, st, "mix", fmt.Sprintf(
		"SELECT tokens_input, cost_usd FROM cardo.gold_model_mix "+
			"WHERE day = '%s' AND source = 'sessions' AND model = 'm1'", day))
	expect(t, "model mix", mix, map[string]string{"tokens_input": "100", "cost_usd": "0.25"})
}
