package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
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

// expectedTeamWeeks is the rows the weekly team views should hold: a team that had at least the
// minimum of people that week, or every smaller team that week added up as "other".
func (c *madeUpCheck) expectedTeamWeeks() map[string]*cohortWeek {
	out := map[string]*cohortWeek{}
	for _, w := range c.key.Weeks {
		for _, x := range w.Teams {
			cohort := x.Team
			if x.People < c.key.MinGroupSize {
				cohort = "other"
			}
			e := out[w.Week+"|"+cohort]
			if e == nil {
				e = &cohortWeek{}
				out[w.Week+"|"+cohort] = e
			}
			e.teams++
			e.People += x.People
			e.Sessions += x.Sessions
			e.Requests += x.Requests
			e.CostUSD += x.CostUSD
			e.Instructions = append(e.Instructions, x.Instructions...)
		}
	}
	return out
}

type cohortWeek struct {
	madeup.KeyTeamWeek
	teams int
}

// currentVersions is, by week and file, the highest version anyone had loaded by the end of
// that week: the version the views call current. names is the files a week loaded: by the week
// of the load in gold_instructions_versions, and of the session in the view by team.
func (c *madeUpCheck) currentVersions(names func(madeup.KeyWeek) []string) map[string]int {
	out := map[string]int{}
	highest := map[string]int{}
	for _, w := range c.key.Weeks { // in order
		for _, name := range names(w) {
			family, v := splitVersion(name)
			highest[family] = max(highest[family], v)
		}
		for family, v := range highest {
			out[w.Week+"|"+family] = v
		}
	}
	return out
}

func loadedThatWeek(w madeup.KeyWeek) (names []string) {
	for _, a := range w.Artifacts {
		if a.Kind == "instructions" && a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return names
}

func loadedBySessionsThatWeek(w madeup.KeyWeek) (names []string) {
	for _, x := range w.Teams {
		for _, l := range x.Instructions {
			names = append(names, l.Name)
		}
	}
	return names
}

var versioned = regexp.MustCompile(`^(.*)-v([0-9]+)([.][A-Za-z0-9]+)?$`)

// splitVersion reads a file's name as the views do: example-engineering-standards-v3.md is
// version 3 of example-engineering-standards. A name with no version is version 0 of itself.
func splitVersion(name string) (family string, version int) {
	m := versioned.FindStringSubmatch(name)
	if m == nil {
		return name, 0
	}
	v, _ := strconv.Atoi(m[2])
	return m[1], v
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
			people, sessions, uses, active, teams int
			first                                 string
		}
		first := map[[2]string]string{}
		for _, w := range c.key.Weeks {
			for _, a := range w.Artifacts {
				if f, ok := first[[2]string{a.Kind, a.Name}]; !ok || w.Week < f {
					first[[2]string{a.Kind, a.Name}] = w.Week
				}
			}
		}
		want := map[string]row{}
		for _, w := range c.key.Weeks {
			names := map[[2]string]int{}
			for _, a := range w.Artifacts {
				names[[2]string{a.Kind, a.Name}] += a.People
			}
			for _, a := range w.Artifacts {
				if a.Org || a.Builtin || a.Name == "" || names[[2]string{a.Kind, a.Name}] >= c.key.MinGroupSize {
					want[strings.Join([]string{w.Week, a.Kind, a.Name, a.FileKind}, "|")] = row{a.People, a.Sessions, a.Uses,
						w.ActivePeople, a.Teams, first[[2]string{a.Kind, a.Name}]}
				}
			}
			for _, a := range w.Other {
				want[strings.Join([]string{w.Week, a.Kind, "other", ""}, "|")] = row{a.People, a.Sessions, a.Uses, w.ActivePeople, a.Teams, "0"}
			}
		}
		seen := map[string]bool{}
		for _, got := range c.rows(t, "SELECT week, kind, artifact, file_kind, people, sessions, uses, active_people, "+
			"teams, first_week FROM cardo.gold_artifact_usage ORDER BY week, kind, artifact, file_kind") {
			k := strings.Join([]string{got["week"], got["kind"], got["artifact"], got["file_kind"]}, "|")
			seen[k] = true
			e, ok := want[k]
			if !ok {
				r.diff("%s: a row the key does not have (%s people)", k, got["people"])
				continue
			}
			r.checked++
			g := fmt.Sprintf("%s people, %s sessions, %s uses, %s active, %s teams, first used %s", got["people"],
				got["sessions"], got["uses"], got["active_people"], got["teams"], got["first_week"])
			if w := fmt.Sprintf("%d people, %d sessions, %d uses, %d active, %d teams, first used %s", e.people,
				e.sessions, e.uses, e.active, e.teams, e.first); g != w {
				r.diff("%s: %s;\n      the key has %s", k, g, w)
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
		first := map[string]string{}
		current := c.currentVersions(loadedThatWeek)
		for _, w := range c.key.Weeks {
			for _, a := range w.Artifacts {
				if a.Kind == "instructions" && a.Name != "" {
					want[w.Week+"|"+a.Name] = fmt.Sprintf("%d people, %d sessions, %d loads", a.People, a.Sessions, a.Uses)
					if _, ok := first[a.Name]; !ok {
						first[a.Name] = w.Week
					}
				}
			}
		}
		seen := map[string]bool{}
		for _, got := range c.rows(t, "SELECT week, family, instructions_name, version, is_current, people, sessions, "+
			"loads, first_week FROM cardo.gold_instructions_versions ORDER BY week, instructions_name") {
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
			cur := current[got["week"]+"|"+got["family"]]
			if isCurrent := got["version"] == strconv.Itoa(cur); (got["is_current"] == "1") != isCurrent {
				r.diff("%s: is_current is %s; the highest version loaded by that week is %d", k, got["is_current"], cur)
			}
			if got["first_week"] != first[got["instructions_name"]] {
				r.diff("%s: first loaded %s; the key has %s", k, got["first_week"], first[got["instructions_name"]])
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	teams := c.expectedTeamWeeks()

	t.Run("gold_spend_weekly and gold_context_weekly", func(t *testing.T) {
		r := &report{t: t, view: "gold_spend_weekly and gold_context_weekly"}
		rows := map[string]map[string]string{}
		for _, row := range c.rows(t, "SELECT week, cohort, sessions AS spend_sessions, requests, cost_usd FROM cardo.gold_spend_weekly") {
			rows[row["week"]+"|"+row["cohort"]] = row
		}
		for _, row := range c.rows(t, "SELECT week, cohort, active_people, sessions FROM cardo.gold_context_weekly") {
			k := row["week"] + "|" + row["cohort"]
			if rows[k] == nil {
				rows[k] = map[string]string{"spend_sessions": "none", "requests": "none", "cost_usd": "0"}
			}
			maps.Copy(rows[k], row)
		}
		seen := map[string]bool{}
		for _, k := range slices.Sorted(maps.Keys(rows)) {
			row := rows[k]
			seen[k] = true
			e := teams[k]
			if e == nil {
				r.diff("%s: a row the key has no people for", k)
				continue
			}
			r.checked++
			got := fmt.Sprintf("%s people, %s sessions, %s with requests, %s requests", row["active_people"],
				row["sessions"], row["spend_sessions"], row["requests"])
			want := fmt.Sprintf("%d people, %d sessions, %d with requests, %d requests", e.People, e.Sessions, e.Sessions, e.Requests)
			if got != want {
				r.diff("%s: %s; the key has %s", k, got, want)
			}
			if math.Abs(num(row["cost_usd"])-e.CostUSD) > 0.0001*float64(e.teams)+1e-6 {
				r.diff("%s: $%s; the key has $%.6f", k, row["cost_usd"], e.CostUSD)
			}
		}
		for k := range teams {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	t.Run("gold_instructions_by_team_weekly", func(t *testing.T) {
		r := &report{t: t, view: "gold_instructions_by_team_weekly"}
		current := c.currentVersions(loadedBySessionsThatWeek)
		want := map[string][2]int{}
		for wc, e := range teams {
			for _, l := range e.Instructions {
				family, version := splitVersion(l.Name)
				k := wc + "|" + family
				v := want[k]
				v[0] += l.Sessions
				if version == current[strings.SplitN(wc, "|", 2)[0]+"|"+family] {
					v[1] += l.Sessions
				}
				want[k] = v
			}
		}
		seen := map[string]bool{}
		for _, row := range c.rows(t, "SELECT week, cohort, family, sessions, sessions_current "+
			"FROM cardo.gold_instructions_by_team_weekly ORDER BY week, family, cohort") {
			k := row["week"] + "|" + row["cohort"] + "|" + row["family"]
			seen[k] = true
			e, ok := want[k]
			if !ok {
				r.diff("%s: a row the key does not have", k)
				continue
			}
			r.checked++
			if got, w := row["sessions"]+" sessions, "+row["sessions_current"]+" on the current version",
				fmt.Sprintf("%d sessions, %d on the current version", e[0], e[1]); got != w {
				r.diff("%s: %s; the key has %s", k, got, w)
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	t.Run("gold_setup_context_weekly", func(t *testing.T) {
		r := &report{t: t, view: "gold_setup_context_weekly"}
		want := map[string]string{}
		for _, w := range c.key.Weeks {
			if w.SessionsWithRequests > 0 {
				want[w.Week] = fmt.Sprintf("%d sessions, %d tokens at the median, %d at the 90th percentile",
					w.SessionsWithRequests, w.StartContextP50, w.StartContextP90)
			}
		}
		seen := map[string]bool{}
		for _, row := range c.rows(t, "SELECT week, sessions_with_requests, session_start_context_p50, "+
			"session_start_context_p90 FROM cardo.gold_setup_context_weekly ORDER BY week") {
			seen[row["week"]] = true
			e, ok := want[row["week"]]
			if !ok {
				r.diff("%s: a row the key does not have", row["week"])
				continue
			}
			r.checked++
			if got := fmt.Sprintf("%s sessions, %s tokens at the median, %s at the 90th percentile", row["sessions_with_requests"],
				row["session_start_context_p50"], row["session_start_context_p90"]); got != e {
				r.diff("%s: %s; the key has %s", row["week"], got, e)
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	// A model, release or MCP tool fewer than the minimum of people used in a week is folded into
	// one row. Its people are compared only when one name alone is folded: a person can be in two.
	type folded struct {
		people, sessions, requests, calls, loops, failed, loopSessions, names int
		cost                                                                  float64
		buggy                                                                 bool
	}
	fold := func(m map[string]*folded, week, name, other string, people int, f folded) {
		if people < c.key.MinGroupSize {
			name = other
		}
		e := m[week+"|"+name]
		if e == nil {
			e = &folded{buggy: f.buggy}
			m[week+"|"+name] = e
		}
		e.names++
		e.people += people
		e.sessions += f.sessions
		e.requests += f.requests
		e.calls += f.calls
		e.loops += f.loops
		e.failed += f.failed
		e.loopSessions += f.loopSessions
		e.cost += f.cost
		e.buggy = e.buggy && f.buggy
	}
	people := func(e *folded) string {
		if e.names > 1 {
			return "-"
		}
		return strconv.Itoa(e.people)
	}

	t.Run("gold_model_spend_weekly", func(t *testing.T) {
		r := &report{t: t, view: "gold_model_spend_weekly"}
		want := map[string]*folded{}
		for _, w := range c.key.Weeks {
			for _, m := range w.Models {
				fold(want, w.Week, m.Model, "other models", m.People, folded{requests: m.Requests, cost: m.CostUSD})
			}
		}
		seen := map[string]bool{}
		for _, row := range c.rows(t, "SELECT week, model, people, requests, cost_usd FROM cardo.gold_model_spend_weekly ORDER BY week, model") {
			k := row["week"] + "|" + row["model"]
			seen[k] = true
			e := want[k]
			if e == nil {
				r.diff("%s: a row the key does not have", k)
				continue
			}
			r.checked++
			gotPeople := row["people"]
			if e.names > 1 {
				gotPeople = "-"
			}
			if got, w := gotPeople+" people, "+row["requests"]+" requests", people(e)+" people, "+strconv.Itoa(e.requests)+" requests"; got != w {
				r.diff("%s: %s; the key has %s", k, got, w)
			}
			if math.Abs(num(row["cost_usd"])-e.cost) > 0.0001*float64(e.names)+1e-6 {
				r.diff("%s: $%s; the key has $%.6f", k, row["cost_usd"], e.cost)
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		r.done()
	})

	t.Run("gold_release_cache_weekly", func(t *testing.T) {
		r := &report{t: t, view: "gold_release_cache_weekly"}
		want := map[string]*folded{}
		for _, w := range c.key.Weeks {
			for _, x := range w.Releases {
				fold(want, w.Week, x.Version, "other releases", x.People, folded{sessions: x.Sessions, requests: x.Requests, buggy: x.Buggy})
			}
		}
		seen := map[string]bool{}
		type share struct {
			release string
			share   float64
		}
		buggy, fixed := map[string][]share{}, map[string][]share{}
		for _, row := range c.rows(t, "SELECT week, app_version, people, sessions, requests, cache_read_share "+
			"FROM cardo.gold_release_cache_weekly ORDER BY week, app_version") {
			k := row["week"] + "|" + row["app_version"]
			seen[k] = true
			e := want[k]
			if e == nil {
				r.diff("%s: a row the key does not have", k)
				continue
			}
			r.checked++
			gotPeople := row["people"]
			if e.names > 1 {
				gotPeople = "-"
			}
			got := fmt.Sprintf("%s people, %s sessions, %s requests", gotPeople, row["sessions"], row["requests"])
			if w := fmt.Sprintf("%s people, %d sessions, %d requests", people(e), e.sessions, e.requests); got != w {
				r.diff("%s: %s; the key has %s", k, got, w)
			}
			if row["app_version"] == "other releases" {
				continue
			}
			sh := share{row["app_version"], num(row["cache_read_share"])}
			if e.buggy {
				buggy[row["week"]] = append(buggy[row["week"]], sh)
			} else {
				fixed[row["week"]] = append(fixed[row["week"]], sh)
			}
		}
		for k := range want {
			if !seen[k] {
				r.diff("%s: missing", k)
			}
		}
		// The caching-bug story, as the release table should show it: in every week, each named
		// release with the bug reads less of its context from the cache than each without it.
		for week, bs := range buggy {
			for _, b := range bs {
				for _, f := range fixed[week] {
					if b.share >= f.share {
						r.diff("week of %s: %s, with the caching bug, reads %.4f of its context from the cache, "+
							"and %s, without it, %.4f", week, b.release, b.share, f.release, f.share)
					}
				}
			}
		}
		if len(buggy) == 0 {
			r.diff("no release with the caching bug is named in any week")
		}
		r.done()
	})

	t.Run("gold_tool_loops_weekly", func(t *testing.T) {
		r := &report{t: t, view: "gold_tool_loops_weekly"}
		want := map[string]*folded{}
		for _, w := range c.key.Weeks {
			for _, x := range w.Tools {
				n := x.People
				if !strings.HasPrefix(x.Tool, "mcp__") {
					n = c.key.MinGroupSize // Claude Code's own tools are always named
				}
				fold(want, w.Week, x.Tool, "other tools", n, folded{calls: x.Calls, loops: x.Loops, failed: x.FailedInLoops,
					loopSessions: x.SessionsWithLoops})
			}
		}
		seen := map[string]bool{}
		for _, row := range c.rows(t, "SELECT week, tool_name, calls, loops, failed_in_loops, sessions_with_loops "+
			"FROM cardo.gold_tool_loops_weekly ORDER BY week, tool_name") {
			k := row["week"] + "|" + row["tool_name"]
			seen[k] = true
			e := want[k]
			if e == nil {
				r.diff("%s: a row the key does not have", k)
				continue
			}
			r.checked++
			// A session with loops in two folded tools is one session in the view.
			sessions, gotSessions := strconv.Itoa(e.loopSessions), row["sessions_with_loops"]
			if e.names > 1 {
				sessions, gotSessions = "-", "-"
			}
			got := fmt.Sprintf("%s calls, %s loops, %s failed in them, in %s sessions", row["calls"], row["loops"],
				row["failed_in_loops"], gotSessions)
			if w := fmt.Sprintf("%d calls, %d loops, %d failed in them, in %s sessions", e.calls, e.loops, e.failed, sessions); got != w {
				r.diff("%s: %s; the key has %s", k, got, w)
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

		// The weekly views: a team, model, release or MCP tool fewer than the minimum used in every
		// week is named in none of them.
		weekly := map[string]int{}
		for _, w := range c.key.Weeks {
			for _, x := range w.Teams {
				weekly["team|"+x.Team] = max(weekly["team|"+x.Team], x.People)
			}
			for _, x := range w.Models {
				weekly["model|"+x.Model] = max(weekly["model|"+x.Model], x.People)
			}
			for _, x := range w.Releases {
				weekly["release|"+x.Version] = max(weekly["release|"+x.Version], x.People)
			}
			for _, x := range w.Tools {
				if strings.HasPrefix(x.Tool, "mcp__") {
					weekly["tool|"+x.Tool] = max(weekly["tool|"+x.Tool], x.People)
				}
			}
		}
		named = map[string]bool{}
		for _, row := range c.rows(t, "SELECT DISTINCT concat('team|', cohort) AS name FROM cardo.gold_spend_weekly UNION DISTINCT "+
			"SELECT DISTINCT concat('team|', cohort) FROM cardo.gold_context_weekly UNION DISTINCT "+
			"SELECT DISTINCT concat('team|', cohort) FROM cardo.gold_instructions_by_team_weekly UNION DISTINCT "+
			"SELECT DISTINCT concat('model|', model) FROM cardo.gold_model_spend_weekly UNION DISTINCT "+
			"SELECT DISTINCT concat('release|', app_version) FROM cardo.gold_release_cache_weekly UNION DISTINCT "+
			"SELECT DISTINCT concat('tool|', tool_name) FROM cardo.gold_tool_loops_weekly") {
			named[row["name"]] = true
		}
		hidden := 0
		for name, n := range weekly {
			if n < c.key.MinGroupSize {
				hidden++
				if named[name] {
					t.Errorf("a weekly view names %q, which at most %d people used in a week", name, n)
				}
			}
		}
		t.Logf("weekly views: %d teams, models, releases and connector tools under the minimum in every week, named nowhere", hidden)
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
