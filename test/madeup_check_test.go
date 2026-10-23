package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/madeup"
	"github.com/amanachenko/cardo/internal/store/clickhouse"
)

// The check (ADR-0051, decision 5): each gold view on the made-up stack, against the answer key
// the generator wrote when it loaded that stack. It holds at any size, because the key records
// what was sent rather than what a story was meant to look like.
//
// It reads only the made-up stack, and only after a single load: scripts/made-up.sh check runs it
// with CARDO_MADEUP_URL set to that stack's ClickHouse. Run against a stack loaded twice, or
// probed since, the counts are off and it says so.

type madeUpCheck struct {
	ch  *clickhouse.Client
	key madeup.Key
}

func madeUpStack(t *testing.T) *madeUpCheck {
	t.Helper()
	raw := os.Getenv("CARDO_MADEUP_URL")
	if raw == "" {
		t.Skip("set CARDO_MADEUP_URL to the made-up stack's ClickHouse to check its views against " +
			"the answer key (scripts/made-up.sh check)")
	}
	// The made-up stack's ClickHouse, and nothing else: the port docker-compose.made-up.yml
	// publishes, on loopback.
	m := regexp.MustCompile(`"(127\.0\.0\.1:\d+):8123"`).FindStringSubmatch(madeUpFile(t, "docker-compose.made-up.yml"))
	if m == nil {
		t.Fatal("docker-compose.made-up.yml publishes no ClickHouse port")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host != m[1] || strings.Trim(u.Path, "/") != "" {
		t.Fatalf("CARDO_MADEUP_URL is %q; this check reads only the made-up stack's ClickHouse, "+
			"http://%s", raw, m[1])
	}

	path := os.Getenv("CARDO_MADEUP_KEY")
	if path == "" {
		path = filepath.Join(repoRoot(t), "deploy", "compose", "made-up", "loaded", "answer-key.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the answer key: %v. scripts/made-up.sh load writes it.", err)
	}
	c := &madeUpCheck{}
	if err := json.Unmarshal(b, &c.key); err != nil {
		t.Fatal(err)
	}

	user := os.Getenv("CARDO_MADEUP_USER")
	if user == "" {
		user = "cardo"
	}
	c.ch, err = clickhouse.NewClient(clickhouse.Config{
		Endpoint: raw, User: user, Password: os.Getenv("CARDO_MADEUP_PASSWORD"), Timeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.ch.Close() })

	settings := c.rows(t, "SELECT min_group_size, org_artifacts FROM cardo.settings_effective")
	if len(settings) != 1 || settings[0]["min_group_size"] != strconv.Itoa(c.key.MinGroupSize) ||
		settings[0]["org_artifacts"] != c.key.OrgArtifacts {
		t.Fatalf("the stack's views run with %v; the key was written for a minimum of %d and the "+
			"pattern %q", settings, c.key.MinGroupSize, c.key.OrgArtifacts)
	}
	probes := c.rows(t, "SELECT count() AS n FROM cardo.bronze_hook_events WHERE startsWith(LogAttributes['session_id'], 'made-up-probe-')")
	if probes[0]["n"] != "0" {
		t.Fatal("scripts/made-up.sh probe was run on this stack after its load, and its rows count in " +
			"the views. Run down, up and load again.")
	}
	return c
}

// rows runs a query and returns its rows by column name. NULL reads as "0": every column
// compared here is a count or a sum that a LEFT JOIN leaves NULL when there was nothing.
func (c *madeUpCheck) rows(t *testing.T, query string) []map[string]string {
	t.Helper()
	out, err := c.ch.Query(context.Background(), query+" FORMAT TSVWithNames")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var names []string
	var rows []map[string]string
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if names == nil {
			names = cols
			continue
		}
		row := map[string]string{}
		for i, v := range cols {
			if v == `\N` {
				v = "0"
			}
			row[names[i]] = v
		}
		rows = append(rows, row)
	}
	return rows
}

// cohortDay is one row the daily views should hold: a team that had at least the minimum of
// people that day, or every smaller team that day added up as "other".
type cohortDay struct {
	madeup.KeyDay
	teams int
}

func (c *madeUpCheck) expectedDays() map[string]*cohortDay {
	out := map[string]*cohortDay{}
	for _, d := range c.key.Days {
		cohort := d.Team
		if d.People < c.key.MinGroupSize {
			cohort = "other"
		}
		e := out[d.Day+"|"+cohort]
		if e == nil {
			e = &cohortDay{KeyDay: madeup.KeyDay{Day: d.Day, Team: cohort}}
			out[d.Day+"|"+cohort] = e
		}
		e.teams++
		e.People += d.People
		e.Sessions += d.Sessions
		e.Prompts += d.Prompts
		e.CostUSD += d.CostUSD
		e.SessionsWithRequests += d.SessionsWithRequests
		e.StartContextP50 = d.StartContextP50 // compared only for a team alone in its row
		e.EditProposals += d.EditProposals
		e.EditRejections += d.EditRejections
		e.PermissionPrompts += d.PermissionPrompts
		e.PermissionWaitMs += d.PermissionWaitMs
		e.Compactions += d.Compactions
		e.AutoCompactions += d.AutoCompactions
	}
	return out
}

// report gathers a view's differences from the key, and fails once with the first few of them.
type report struct {
	t       *testing.T
	view    string
	lines   []string
	checked int
}

func (r *report) diff(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *report) done() {
	r.t.Helper()
	if len(r.lines) == 0 {
		r.t.Logf("%s: %d rows, each as the answer key has it", r.view, r.checked)
		return
	}
	shown := r.lines[:min(len(r.lines), 15)]
	r.t.Errorf("%s differs from the answer key in %d places:\n  %s", r.view, len(r.lines), strings.Join(shown, "\n  "))
}

func num(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func TestMadeUpViewsMatchTheAnswerKey(t *testing.T) {
	c := madeUpStack(t)
	expected := c.expectedDays()

	t.Run("gold_context_daily", func(t *testing.T) {
		r := &report{t: t, view: "gold_context_daily"}
		seen := map[string]bool{}
		for _, row := range c.rows(t, "SELECT day, cohort, active_people, sessions, cost_usd, sessions_with_requests, "+
			"session_start_context_p50 FROM cardo.gold_context_daily ORDER BY day, cohort") {
			k := row["day"] + "|" + row["cohort"]
			seen[k] = true
			e := expected[k]
			if e == nil {
				r.diff("%s: a row the key has no people for", k)
				continue
			}
			r.checked++
			got := fmt.Sprintf("%s people, %s sessions, %s with requests", row["active_people"], row["sessions"], row["sessions_with_requests"])
			want := fmt.Sprintf("%d people, %d sessions, %d with requests", e.People, e.Sessions, e.SessionsWithRequests)
			if got != want {
				r.diff("%s: %s; the key has %s", k, got, want)
			}
			if math.Abs(num(row["cost_usd"])-e.CostUSD) > 0.0001*float64(e.teams)+1e-6 {
				r.diff("%s: $%s; the key has $%.6f", k, row["cost_usd"], e.CostUSD)
			}
			if e.teams == 1 && row["session_start_context_p50"] != strconv.Itoa(e.StartContextP50) {
				r.diff("%s: sessions start from a median %s tokens; the key has %d", k, row["session_start_context_p50"], e.StartContextP50)
			}
		}
		for k := range expected {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	t.Run("gold_friction_daily", func(t *testing.T) {
		r := &report{t: t, view: "gold_friction_daily"}
		seen := map[string]bool{}
		for _, row := range c.rows(t, "SELECT day, cohort, active_people, sessions, prompts, edit_proposals, edit_rejections, "+
			"permission_prompts, permission_answered, permission_prompts_two_clocks, permission_wait_s_total, "+
			"compactions, auto_compactions FROM cardo.gold_friction_daily ORDER BY day, cohort") {
			k := row["day"] + "|" + row["cohort"]
			seen[k] = true
			e := expected[k]
			if e == nil {
				r.diff("%s: a row the key has no people for", k)
				continue
			}
			r.checked++
			got := fmt.Sprintf("%s people, %s sessions, %s prompts, %s edits proposed, %s rejected, "+
				"%s permission prompts, %s answered, %s on two clocks, %s compactions, %s automatic",
				row["active_people"], row["sessions"], row["prompts"], row["edit_proposals"], row["edit_rejections"],
				row["permission_prompts"], row["permission_answered"], row["permission_prompts_two_clocks"],
				row["compactions"], row["auto_compactions"])
			want := fmt.Sprintf("%d people, %d sessions, %d prompts, %d edits proposed, %d rejected, "+
				"%d permission prompts, %d answered, 0 on two clocks, %d compactions, %d automatic",
				e.People, e.Sessions, e.Prompts, e.EditProposals, e.EditRejections,
				e.PermissionPrompts, e.PermissionPrompts, e.Compactions, e.AutoCompactions)
			if got != want {
				r.diff("%s: %s;\n      the key has %s", k, got, want)
			}
			// ClickHouse's round() takes a float's half to the even neighbour: 1014.65 s is 1014.6.
			if want := math.RoundToEven(float64(e.PermissionWaitMs)/100) / 10; math.Abs(num(row["permission_wait_s_total"])-want) > 1e-6 {
				r.diff("%s: %s s of permission waits; the key has %.1f s", k, row["permission_wait_s_total"], want)
			}
		}
		for k := range expected {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	t.Run("gold_artifact_usage", func(t *testing.T) {
		r := &report{t: t, view: "gold_artifact_usage"}
		type row struct {
			people, sessions, uses, active int
		}
		want := map[string]row{}
		for _, w := range c.key.Weeks {
			names := map[[2]string]int{}
			for _, a := range w.Artifacts {
				names[[2]string{a.Kind, a.Name}] += a.People
			}
			for _, a := range w.Artifacts {
				if a.Org || a.Builtin || a.Name == "" || names[[2]string{a.Kind, a.Name}] >= c.key.MinGroupSize {
					want[strings.Join([]string{w.Week, a.Kind, a.Name, a.FileKind}, "|")] = row{a.People, a.Sessions, a.Uses, w.ActivePeople}
				}
			}
			for _, a := range w.Other {
				want[strings.Join([]string{w.Week, a.Kind, "other", ""}, "|")] = row{a.People, a.Sessions, a.Uses, w.ActivePeople}
			}
		}
		seen := map[string]bool{}
		for _, got := range c.rows(t, "SELECT week, kind, artifact, file_kind, people, sessions, uses, active_people "+
			"FROM cardo.gold_artifact_usage ORDER BY week, kind, artifact, file_kind") {
			k := strings.Join([]string{got["week"], got["kind"], got["artifact"], got["file_kind"]}, "|")
			seen[k] = true
			e, ok := want[k]
			if !ok {
				r.diff("%s: a row the key does not have (%s people)", k, got["people"])
				continue
			}
			r.checked++
			if g := fmt.Sprintf("%s people, %s sessions, %s uses, %s active", got["people"], got["sessions"], got["uses"], got["active_people"]); g !=
				fmt.Sprintf("%d people, %d sessions, %d uses, %d active", e.people, e.sessions, e.uses, e.active) {
				r.diff("%s: %s; the key has %d people, %d sessions, %d uses, %d active", k, g, e.people, e.sessions, e.uses, e.active)
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	t.Run("gold_instructions_versions", func(t *testing.T) {
		r := &report{t: t, view: "gold_instructions_versions"}
		want := map[string]string{}
		current := 0
		for _, w := range c.key.Weeks {
			for _, a := range w.Artifacts {
				if a.Kind == "instructions" && a.Name != "" {
					want[w.Week+"|"+a.Name] = fmt.Sprintf("%d people, %d sessions, %d loads", a.People, a.Sessions, a.Uses)
					if v, _ := strconv.Atoi(regexp.MustCompile(`-v(\d+)\.`).FindStringSubmatch(a.Name + ".")[1]); v > current {
						current = v
					}
				}
			}
		}
		seen := map[string]bool{}
		for _, got := range c.rows(t, "SELECT week, instructions_name, version, is_current, people, sessions, loads "+
			"FROM cardo.gold_instructions_versions ORDER BY week, instructions_name") {
			k := got["week"] + "|" + got["instructions_name"]
			seen[k] = true
			e, ok := want[k]
			if !ok {
				r.diff("%s: a row the key does not have", k)
				continue
			}
			r.checked++
			if g := fmt.Sprintf("%s people, %s sessions, %s loads", got["people"], got["sessions"], got["loads"]); g != e {
				r.diff("%s: %s; the key has %s", k, g, e)
			}
			if isCurrent := got["version"] == strconv.Itoa(current); (got["is_current"] == "1") != isCurrent {
				r.diff("%s: is_current is %s; the highest version loaded is %d", k, got["is_current"], current)
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	// The five-person minimum, said once more in plain terms: what the key says no view may name
	// is named nowhere.
	t.Run("never named", func(t *testing.T) {
		most := map[string]int{}
		for _, w := range c.key.Weeks {
			for _, a := range w.Artifacts {
				if !a.Org && !a.Builtin && a.Name != "" {
					most[a.Name] = max(most[a.Name], a.People)
				}
			}
		}
		named := map[string]bool{}
		for _, row := range c.rows(t, "SELECT DISTINCT artifact FROM cardo.gold_artifact_usage") {
			named[row["artifact"]] = true
		}
		for name, n := range most {
			if n < c.key.MinGroupSize && named[name] {
				t.Errorf("gold_artifact_usage names %q, which at most %d people used in a week", name, n)
			}
		}
		cohorts := map[string]bool{}
		for _, row := range c.rows(t, "SELECT DISTINCT cohort FROM cardo.gold_context_daily UNION DISTINCT "+
			"SELECT DISTINCT cohort FROM cardo.gold_friction_daily") {
			cohorts[row["cohort"]] = true
		}
		for _, team := range c.key.Teams {
			if team.People < c.key.MinGroupSize && cohorts[team.Name] {
				t.Errorf("the daily views name the team %q, which has %d people", team.Name, team.People)
			}
		}
	})

	// Where each story should show, and what no view shows yet: the list to read the dashboards
	// against.
	var gaps []string
	for _, s := range c.key.Stories {
		where := s.Shows
		if where == "" {
			where = "no view"
		}
		t.Logf("%s: %s\n    shows in: %s", s.Name, s.Planted, where)
		if s.Gap != "" {
			gaps = append(gaps, s.Name+": "+s.Gap)
		}
	}
	slices.Sort(gaps)
	t.Logf("Gaps, %d stories no view shows in full:\n  %s", len(gaps), strings.Join(gaps, "\n  "))
}
