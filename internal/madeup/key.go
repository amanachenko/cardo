package madeup

import (
	"cmp"
	"encoding/json"
	"math"
	"os"
	"slices"
	"time"
)

// Key is the answer key: what the generator put into the made-up organization, counted the way
// Cardo's views count it, so a check can compare each view with it at any size (ADR-0051,
// decision 5).
//
// It counts what the views count, before the five-person minimum: a team or an artifact appears
// here under its own name however few people it has, and the check decides what a view should
// show instead. The one exception is Other, which a check cannot add up from the names alone.
// Days are UTC, as ClickHouse's are.
//
//   - A session belongs to the day of its first OTel record, as silver_session's started_at.
//     Its cost, prompts, edits, permission prompts and compactions are reported on that day, as
//     the gold views report them.
//   - Prompts do not count built-in commands such as /model or /compact, as gold_friction_daily.
//   - An artifact is counted in the week of its own use, as gold_artifact_usage counts it. A use
//     is one load of an instructions file, one subagent started, and one prompt that used a
//     command, skill or MCP server, however many times within the prompt.
type Key struct {
	Note         string      `json:"note"`
	Preset       string      `json:"preset"`
	Seed         uint64      `json:"seed"`
	FirstDay     string      `json:"first_day"`
	LastDay      string      `json:"last_day"`
	OrgArtifacts string      `json:"org_artifacts"`
	MinGroupSize int         `json:"min_group_size"`
	Teams        []KeyTeam   `json:"teams"`
	Styles       []KeyStyle  `json:"styles"`
	Stories      []KeyStory  `json:"stories"`
	People       []KeyPerson `json:"people"`
	Days         []KeyDay    `json:"days"`
	Weeks        []KeyWeek   `json:"weeks"`
	Totals       KeyTotals   `json:"totals"`
}

const keyNote = "Made-up organization: not real data. Every person, team, session and figure here was " +
	"invented by cmd/cardo-madeup from a seed, and the spread of spend and sessions across people is a " +
	"placeholder, not a measurement. The stories were planted, so a view that shows one shows only " +
	"that it was put in. Nothing seen in it is a finding (ADR-0051)."

// MinGroupSize is the five-person minimum the made-up stack runs with: the default, which its
// compose file leaves alone (ADR-0029).
const MinGroupSize = 5

type KeyTeam struct {
	Name   string `json:"name"`
	People int    `json:"people"`
}

type KeyStyle struct {
	Name   string `json:"name"`
	About  string `json:"about"`
	People int    `json:"people"`
}

// KeyStory is one story, with the figures the generator measured in what it sent.
type KeyStory struct {
	Name    string      `json:"name"`
	Planted string      `json:"planted"`
	Shows   string      `json:"shows,omitempty"`
	Gap     string      `json:"gap,omitempty"`
	Figures []KeyFigure `json:"figures,omitempty"`
}

type KeyFigure struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type KeyPerson struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Team  string `json:"team"`
	Style string `json:"style"`
	Model string `json:"model"`
	Mode  string `json:"permission_mode"`
	// The placeholder month the person was drawn with. Sessions and cost below are what was sent.
	PlaceholderMonthlySpend    float64 `json:"placeholder_monthly_spend_usd"`
	PlaceholderMonthlySessions float64 `json:"placeholder_monthly_sessions"`
	FirstDay                   string  `json:"first_day,omitempty"`
	ActiveDays                 int     `json:"active_days"`
	Sessions                   int     `json:"sessions"`
	Prompts                    int     `json:"prompts"`
	Requests                   int     `json:"api_requests"`
	CostUSD                    float64 `json:"cost_usd"`
}

// KeyDay is one team on one day, in the columns gold_context_daily and gold_friction_daily
// publish.
type KeyDay struct {
	Day      string  `json:"day"`
	Team     string  `json:"team"`
	People   int     `json:"people"`
	Sessions int     `json:"sessions"`
	Prompts  int     `json:"prompts"`
	CostUSD  float64 `json:"cost_usd"`
	// Sessions with a main-thread request, and the median context of each one's first, as
	// ClickHouse's quantileExact takes it: the upper of the two middle values.
	SessionsWithRequests int   `json:"sessions_with_requests"`
	StartContextP50      int   `json:"session_start_context_p50"`
	EditProposals        int   `json:"edit_proposals"`
	EditRejections       int   `json:"edit_rejections"`
	PermissionPrompts    int   `json:"permission_prompts"`
	PermissionWaitMs     int64 `json:"permission_wait_ms"`
	Compactions          int   `json:"compactions"`
	AutoCompactions      int   `json:"auto_compactions"`
}

type KeyWeek struct {
	Week         string        `json:"week"`
	ActivePeople int           `json:"active_people"`
	Sessions     int           `json:"sessions"`
	CostUSD      float64       `json:"cost_usd"`
	Artifacts    []KeyArtifact `json:"artifacts"`
	// Other is, for each kind, what the home-grown names fewer than MinGroupSize people used
	// that week add up to, counted as the view counts its "other" row: a person or a session
	// once, however many of those names they used.
	Other []KeyArtifact `json:"other,omitempty"`
}

type KeyArtifact struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	FileKind string `json:"file_kind,omitempty"`
	Org      bool   `json:"org"`
	Builtin  bool   `json:"builtin"`
	People   int    `json:"people"`
	Sessions int    `json:"sessions"`
	Uses     int    `json:"uses"`
}

type KeyTotals struct {
	People       int     `json:"people"`
	Sessions     int     `json:"sessions"`
	Prompts      int     `json:"prompts"`
	Requests     int     `json:"api_requests"`
	CostUSD      float64 `json:"cost_usd"`
	HookEvents   int     `json:"hook_events"`
	LogRecords   int     `json:"log_records"`
	MetricPoints int     `json:"metric_points"`
}

// WriteFile writes the key as indented JSON.
func (k *Key) WriteFile(path string) error {
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// tally is one person's share of the key, kept while their sessions are written.
type tally struct {
	stories                     Stories
	sessions, prompts, requests int
	cost                        float64
	hooks, records, points      int
	days                        map[dayTeam]*dayCount
	weeks                       map[string]*weekCount
	uses                        map[artifactKey]*useCount
	activeDays                  map[string]bool
	// Sums the stories' figures are worked out from, by name (storyFigures).
	story map[string]float64
}

type dayTeam struct{ day, team string }

type dayCount struct {
	sessions, prompts             int
	cost                          float64
	withRequests                  int
	startContexts                 []int
	editProposals, editRejections int
	permissionPrompts             int
	permissionWaitMs              int64
	compactions, autoCompactions  int
}

type weekCount struct {
	sessions int
	cost     float64
}

type artifactKey struct {
	week, kind, name, fileKind string
}

type useCount struct {
	org, builtin bool
	sessions     map[string]bool
	uses         int
}

func newTally(st Stories) *tally {
	return &tally{
		stories:    st,
		days:       map[dayTeam]*dayCount{},
		weeks:      map[string]*weekCount{},
		uses:       map[artifactKey]*useCount{},
		activeDays: map[string]bool{},
		story:      map[string]float64{},
	}
}

// add folds one finished session into the person's tally.
func (t *tally) add(s *sessionTally, p *Person) {
	team := p.Team.Name
	day := s.start.Format(time.DateOnly)
	week := monday(s.start).Format(time.DateOnly)
	t.sessions++
	t.prompts += s.prompts
	t.requests += s.requests
	t.cost += s.cost
	t.hooks += s.hooks
	t.records += s.records
	t.points += s.points
	t.activeDays[day] = true

	d := t.days[dayTeam{day, team}]
	if d == nil {
		d = &dayCount{}
		t.days[dayTeam{day, team}] = d
	}
	d.sessions++
	d.prompts += s.prompts
	d.cost += s.cost
	if s.mainRequests > 0 {
		d.withRequests++
		d.startContexts = append(d.startContexts, s.startContext)
	}
	d.editProposals += s.editProposals
	d.editRejections += s.editRejections
	d.permissionPrompts += s.permissionPrompts
	d.permissionWaitMs += s.permissionWaitMs
	d.compactions += s.compactions
	d.autoCompactions += s.autoCompactions

	w := t.weeks[week]
	if w == nil {
		w = &weekCount{}
		t.weeks[week] = w
	}
	w.sessions++
	w.cost += s.cost

	for _, u := range s.uses {
		k := artifactKey{monday(u.at).Format(time.DateOnly), u.kind, u.name, u.fileKind}
		c := t.uses[k]
		if c == nil {
			c = &useCount{org: u.org, builtin: u.builtin, sessions: map[string]bool{}}
			t.uses[k] = c
		}
		c.sessions[s.id] = true
		c.uses++
	}
	t.addStories(s, p)
}

// addStories adds what a session contributes to the stories' figures.
func (t *tally) addStories(s *sessionTally, p *Person) {
	st, sum := t.stories, t.story
	add := func(key string, v float64) { sum[key] += v }
	side := func(used bool) string {
		if used {
			return "with"
		}
		return "without"
	}
	spent := func(prefix string) {
		add(prefix+"|sessions", 1)
		add(prefix+"|cost", s.cost)
		add(prefix+"|requests", float64(s.requests))
		add(prefix+"|edits", float64(s.acceptedEdits))
	}

	switch s.kind {
	case overnight:
		add("overnight|sessions", 1)
		add("overnight|cost", s.cost)
		add("overnight|hours", s.end.Sub(s.start).Hours())
		return
	case gaming:
		add(key("gaming", s.week, "sessions"), 1)
		add("gaming|cost", s.cost)
		return
	}
	add("worked|cost", s.cost)

	// Cost per accepted edit, with and without each command, among the people who could use it
	// and from the week they could.
	if p.DeployCheckWeek > 0 && s.week >= p.DeployCheckWeek {
		spent("deploy-check|" + side(s.first == "deploy-check"))
	}
	if st.Review > 0 && s.week >= st.Review && !onTeam(st.NoReview, p.Team.Name) && s.first != "deploy-check" {
		spent("review|" + side(s.first == "review"))
	}
	if s.first == "deploy-check" {
		sum[key("deploy-check|team", s.week, p.Team.Name)] = 1
	}
	if st.InstructionsV3 > 0 && s.week >= st.InstructionsV3 && s.standards > 0 {
		v := "v2"
		if s.standards == 3 {
			v = "v3"
		}
		add("standards|"+v+"|sessions", 1)
		add("standards|"+v+"|start-context", float64(s.startContext))
	}
	spent(key("week", s.week))
	if s.buggy {
		add(key("week", s.week, "buggy"), 1)
	}
	add("bug|added", s.bugCost)
	if st.PerfBudget > 0 && (s.week == st.PerfBudget-1 || s.week == st.PerfBudget) {
		group := "rest"
		if onTeam(st.PerfBudgetTeams, p.Team.Name) {
			group = "teams"
		}
		spent(key("overlap", s.week, group))
	}
	add("flaky|loops", float64(s.retryLoops))
	add("flaky|failed", float64(s.failedRuns))
	add("flaky|requests", float64(s.retryRequests))
	add("flaky|cost", s.retryCost)
	add("style|"+p.Style.Name+"|sessions", 1)
	add("style|"+p.Style.Name+"|compactions", float64(s.compactions))
	add("mode|"+p.Mode+"|prompts", float64(s.permissionPrompts))
	add("mode|"+p.Mode+"|wait-ms", float64(s.permissionWaitMs))
}

// buildKey merges every person's tally into the key.
func buildKey(o *Org, tallies []*tally) *Key {
	k := &Key{
		Note:         keyNote,
		Preset:       o.Preset.Name,
		Seed:         o.Seed,
		FirstDay:     o.Start.Format(time.DateOnly),
		LastDay:      o.End().AddDate(0, 0, -1).Format(time.DateOnly),
		OrgArtifacts: OrgArtifacts,
		MinGroupSize: MinGroupSize,
	}
	for _, t := range o.Preset.Teams {
		k.Teams = append(k.Teams, KeyTeam{t.Name, t.People})
	}
	styleCount := map[string]int{}
	for _, p := range o.People {
		styleCount[p.Style.Name]++
	}
	for _, s := range Styles {
		if styleCount[s.Name] > 0 {
			k.Styles = append(k.Styles, KeyStyle{s.Name, s.About, styleCount[s.Name]})
		}
	}

	days := map[dayTeam]*KeyDay{}
	contexts := map[dayTeam][]int{}
	weeks := map[string]*KeyWeek{}
	weekPeople := map[string]int{}
	arts := map[artifactKey]*KeyArtifact{}
	sums := map[string]float64{}
	for i, p := range o.People {
		t := tallies[i]
		kp := KeyPerson{
			ID: p.ID, Email: p.Email, Team: p.Team.Name, Style: p.Style.Name, Model: p.Model, Mode: p.Mode,
			PlaceholderMonthlySpend:    math.Round(p.MonthlySpend),
			PlaceholderMonthlySessions: math.Round(p.MonthlySessions),
			ActiveDays:                 len(t.activeDays),
			Sessions:                   t.sessions, Prompts: t.prompts, Requests: t.requests,
			CostUSD: round6(t.cost),
		}
		if p.JoinDay > 0 {
			kp.FirstDay = o.Start.AddDate(0, 0, p.JoinDay).Format(time.DateOnly)
		}
		k.People = append(k.People, kp)
		k.Totals.Sessions += t.sessions
		k.Totals.Prompts += t.prompts
		k.Totals.Requests += t.requests
		k.Totals.CostUSD += t.cost
		k.Totals.HookEvents += t.hooks
		k.Totals.LogRecords += t.records
		k.Totals.MetricPoints += t.points
		if t.sessions > 0 {
			k.Totals.People++
		}
		for name, v := range t.story {
			sums[name] += v
		}

		for dt, c := range t.days {
			d := days[dt]
			if d == nil {
				d = &KeyDay{Day: dt.day, Team: dt.team}
				days[dt] = d
			}
			d.People++
			d.Sessions += c.sessions
			d.Prompts += c.prompts
			d.CostUSD += c.cost
			d.SessionsWithRequests += c.withRequests
			d.EditProposals += c.editProposals
			d.EditRejections += c.editRejections
			d.PermissionPrompts += c.permissionPrompts
			d.PermissionWaitMs += c.permissionWaitMs
			d.Compactions += c.compactions
			d.AutoCompactions += c.autoCompactions
			contexts[dt] = append(contexts[dt], c.startContexts...)
		}
		for wk, c := range t.weeks {
			w := weeks[wk]
			if w == nil {
				w = &KeyWeek{Week: wk}
				weeks[wk] = w
			}
			weekPeople[wk]++
			w.Sessions += c.sessions
			w.CostUSD += c.cost
		}
		for ak, c := range t.uses {
			a := arts[ak]
			if a == nil {
				a = &KeyArtifact{Kind: ak.kind, Name: ak.name, FileKind: ak.fileKind, Org: c.org, Builtin: c.builtin}
				arts[ak] = a
			}
			a.People++
			a.Sessions += len(c.sessions)
			a.Uses += c.uses
		}
	}
	k.Totals.CostUSD = round6(k.Totals.CostUSD)

	for dt, d := range days {
		d.CostUSD = round6(d.CostUSD)
		d.StartContextP50 = quantileExact(contexts[dt], 0.5)
		k.Days = append(k.Days, *d)
	}
	slices.SortFunc(k.Days, func(a, b KeyDay) int { return cmp.Or(cmp.Compare(a.Day, b.Day), cmp.Compare(a.Team, b.Team)) })

	for ak, a := range arts {
		w := weeks[ak.week]
		if w == nil {
			// An artifact used just after midnight on a Monday, in a session that started on the
			// Sunday: the week has uses but no session of its own.
			w = &KeyWeek{Week: ak.week}
			weeks[ak.week] = w
		}
		w.Artifacts = append(w.Artifacts, *a)
	}
	for wk, other := range hiddenNames(tallies, arts) {
		w := weeks[wk]
		for _, kind := range sortedKeys(other) {
			w.Other = append(w.Other, *other[kind])
		}
	}
	for wk, w := range weeks {
		w.ActivePeople = weekPeople[wk]
		w.CostUSD = round6(w.CostUSD)
		slices.SortFunc(w.Artifacts, func(a, b KeyArtifact) int {
			return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(b.People, a.People),
				cmp.Compare(a.Name, b.Name), cmp.Compare(a.FileKind, b.FileKind))
		})
		k.Weeks = append(k.Weeks, *w)
	}
	slices.SortFunc(k.Weeks, func(a, b KeyWeek) int { return cmp.Compare(a.Week, b.Week) })
	k.Stories = storyFigures(o, k, sums)
	return k
}

// hiddenNames adds up, per week and kind, the home-grown names fewer than MinGroupSize people
// used that week, as the view's "other" row counts them. The view decides by name alone, so
// one name's people are counted across its file kinds.
func hiddenNames(tallies []*tally, arts map[artifactKey]*KeyArtifact) map[string]map[string]*KeyArtifact {
	type weekName struct{ week, kind, name string }
	people := map[weekName]int{}
	for ak, a := range arts {
		people[weekName{ak.week, ak.kind, ak.name}] += a.People
	}
	hidden := func(ak artifactKey, c *useCount) bool {
		return !c.org && !c.builtin && ak.name != "" && people[weekName{ak.week, ak.kind, ak.name}] < MinGroupSize
	}
	out := map[string]map[string]*KeyArtifact{}
	for _, t := range tallies {
		type weekKind struct{ week, kind string }
		sessions := map[weekKind]map[string]bool{}
		uses := map[weekKind]int{}
		for ak, c := range t.uses {
			if !hidden(ak, c) {
				continue
			}
			wk := weekKind{ak.week, ak.kind}
			if sessions[wk] == nil {
				sessions[wk] = map[string]bool{}
			}
			for id := range c.sessions {
				sessions[wk][id] = true
			}
			uses[wk] += c.uses
		}
		for wk, ids := range sessions {
			if out[wk.week] == nil {
				out[wk.week] = map[string]*KeyArtifact{}
			}
			a := out[wk.week][wk.kind]
			if a == nil {
				a = &KeyArtifact{Kind: wk.kind, Name: "other"}
				out[wk.week][wk.kind] = a
			}
			a.People++
			a.Sessions += len(ids)
			a.Uses += uses[wk]
		}
	}
	return out
}

// quantileExact is ClickHouse's quantileExact: the value at position floor(level * n) of the
// sorted values, so the median of an even count is the upper of the two middle ones.
func quantileExact(values []int, level float64) int {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[min(len(sorted)-1, int(level*float64(len(sorted))))]
}

func monday(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func round6(f float64) float64 { return math.Round(f*1e6) / 1e6 }
