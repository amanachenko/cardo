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
// show instead. Days are UTC, as ClickHouse's are.
//
//   - A session belongs to the day of its first OTel record, as silver_session's started_at.
//     Its cost and prompts are reported on that day, as the gold views report them.
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
	Teams        []KeyTeam   `json:"teams"`
	Styles       []KeyStyle  `json:"styles"`
	People       []KeyPerson `json:"people"`
	Days         []KeyDay    `json:"days"`
	Weeks        []KeyWeek   `json:"weeks"`
	Totals       KeyTotals   `json:"totals"`
}

const keyNote = "Made-up organization: not real data. Every person, team, session and figure here was " +
	"invented by cmd/cardo-madeup from a seed, and the spread of spend and sessions across people is a " +
	"placeholder, not a measurement. Nothing seen in it is a finding (ADR-0051)."

type KeyTeam struct {
	Name   string `json:"name"`
	People int    `json:"people"`
}

type KeyStyle struct {
	Name   string `json:"name"`
	About  string `json:"about"`
	People int    `json:"people"`
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

type KeyDay struct {
	Day      string  `json:"day"`
	Team     string  `json:"team"`
	People   int     `json:"people"`
	Sessions int     `json:"sessions"`
	Prompts  int     `json:"prompts"`
	CostUSD  float64 `json:"cost_usd"`
}

type KeyWeek struct {
	Week         string        `json:"week"`
	ActivePeople int           `json:"active_people"`
	Sessions     int           `json:"sessions"`
	CostUSD      float64       `json:"cost_usd"`
	Artifacts    []KeyArtifact `json:"artifacts"`
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
	sessions, prompts, requests int
	cost                        float64
	hooks, records, points      int
	days                        map[dayTeam]*dayCount
	weeks                       map[string]*weekCount
	uses                        map[artifactKey]*useCount
	activeDays                  map[string]bool
}

type dayTeam struct{ day, team string }

type dayCount struct {
	sessions, prompts int
	cost              float64
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

func newTally() *tally {
	return &tally{
		days:       map[dayTeam]*dayCount{},
		weeks:      map[string]*weekCount{},
		uses:       map[artifactKey]*useCount{},
		activeDays: map[string]bool{},
	}
}

// add folds one finished session into the person's tally.
func (t *tally) add(s *sessionTally, team string) {
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
	weeks := map[string]*KeyWeek{}
	weekPeople := map[string]int{}
	arts := map[artifactKey]*KeyArtifact{}
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

	for _, d := range days {
		d.CostUSD = round6(d.CostUSD)
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
	return k
}

func monday(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func round6(f float64) float64 { return math.Round(f*1e6) / 1e6 }
