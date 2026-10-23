package madeup_test

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/madeup"
)

// A small organization, so the tests decode every request it sends in a few seconds.
var small = madeup.Preset{
	Name:  "test",
	Weeks: 2,
	Teams: []madeup.TeamSpec{
		{Name: "alpha", People: 9, UTC: 1, Repos: []string{"alpha-api"}},
		{Name: "beta", People: 7, UTC: -5, Repos: []string{"beta-app", "beta-lib"}},
		{Name: "gamma", People: 3, UTC: 0, Repos: []string{"gamma-tool"}},
	},
	// Every story, squeezed into two weeks, so the requests decoded below include each one.
	Stories: madeup.Stories{
		DeployCheck:    []madeup.Spread{{Week: 1, Team: "alpha", People: 3}, {Week: 2, Team: "beta", People: 3}},
		InstructionsV3: 2, StayOnV2: []string{"gamma"},
		Review: 2, NoReview: []string{"gamma"},
		Connector:      2,
		ExpensiveModel: 2, ExpensiveTeam: "beta",
		Bug:        [2]int{1, 1},
		PerfBudget: 2, PerfBudgetTeams: []string{"alpha"},
		FlakyTeam: "alpha",
		Gamer:     2, GamerTeam: "alpha",
		Overnight: 1,
	},
}

var start = time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)

func generate(t *testing.T, p madeup.Preset, weeks, workers int, sink madeup.Sink) *madeup.Key {
	t.Helper()
	o, err := madeup.NewOrg(p, 1, start, weeks)
	if err != nil {
		t.Fatal(err)
	}
	k, err := madeup.Generate(context.Background(), o, sink, workers)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// The same seed gives the same organization, whatever the parallelism: the same requests, with
// the same bodies and times, and the same answer key.
func TestSameSeedSameOrganization(t *testing.T) {
	a, b := &hashSink{}, &hashSink{}
	ka := generate(t, small, 2, 1, a)
	kb := generate(t, small, 2, 7, b)
	if !slices.Equal(a.sorted(), b.sorted()) {
		t.Errorf("two runs with seed 1 sent different requests (%d and %d)", len(a.sums), len(b.sums))
	}
	ja, _ := json.Marshal(ka)
	jb, _ := json.Marshal(kb)
	if !bytes.Equal(ja, jb) {
		t.Error("two runs with seed 1 wrote different answer keys")
	}

	c := &hashSink{}
	o, _ := madeup.NewOrg(small, 2, start, 2)
	if _, err := madeup.Generate(context.Background(), o, c, 4); err != nil {
		t.Fatal(err)
	}
	if slices.Equal(a.sorted(), c.sorted()) {
		t.Error("seed 2 sent exactly what seed 1 did")
	}
}

type hashSink struct {
	mu   sync.Mutex
	sums []string
}

func (h *hashSink) add(parts ...[]byte) error {
	s := sha256.New()
	for _, p := range parts {
		s.Write(p)
	}
	h.mu.Lock()
	h.sums = append(h.sums, hex.EncodeToString(s.Sum(nil)))
	h.mu.Unlock()
	return nil
}

func (h *hashSink) Hook(at time.Time, body []byte) error {
	return h.add([]byte(strconv.FormatInt(at.UnixNano(), 10)), body)
}
func (h *hashSink) Logs(body []byte) error    { return h.add([]byte("logs"), body) }
func (h *hashSink) Metrics(body []byte) error { return h.add([]byte("metrics"), body) }
func (h *hashSink) sorted() []string          { out := slices.Clone(h.sums); slices.Sort(out); return out }

// ---- Everything below reads what was sent, as the collector and the views would -----------

var (
	wireOnce sync.Once
	wireKey  *madeup.Key
	wire     *wireSink
)

func sent(t *testing.T) (*madeup.Key, *wireSink) {
	t.Helper()
	wireOnce.Do(func() {
		wire = newWireSink()
		wireKey = generate(t, small, 2, 4, wire)
	})
	if wire.err != nil {
		t.Fatal(wire.err)
	}
	return wireKey, wire
}

var emailShaped = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)

// Every email address sent is at example.com, a domain reserved for examples, and it is sent only
// where Claude Code sends one: as user.email on OTel. No hook payload carries one.
func TestEveryEmailIsAnExample(t *testing.T) {
	_, w := sent(t)
	if w.hooksWithEmail > 0 {
		t.Errorf("%d hook payloads carry an email address; Claude Code's never do, and the collector "+
			"would refuse them", w.hooksWithEmail)
	}
	if len(w.emails) == 0 {
		t.Fatal("no email address was sent at all; the OTel records need user.email to be pseudonymized")
	}
	for e := range w.emails {
		if !strings.HasSuffix(e, "@example.com") {
			t.Errorf("sent %q: every made-up address must be at example.com", e)
		}
	}
	for e := range w.otherEmails {
		t.Errorf("sent %q somewhere other than user.email", e)
	}
}

// Every hook payload has the field names, in order, of a payload recorded from a real Claude Code
// (test/fixtures/hooks/), and every event the managed-settings bundle hooks is sent.
func TestHookPayloadsHaveTheObservedShapes(t *testing.T) {
	_, w := sent(t)
	observed := map[string]map[string]bool{}
	dirs, _ := filepath.Glob(filepath.Join("..", "..", "test", "fixtures", "hooks", "2.1.*"))
	if len(dirs) == 0 {
		t.Fatal("no recorded fixtures found under test/fixtures/hooks/")
	}
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Name string `json:"hook_event_name"`
			}
			json.Unmarshal(b, &event)
			if observed[event.Name] == nil {
				observed[event.Name] = map[string]bool{}
			}
			observed[event.Name][strings.Join(keyOrder(t, b), ",")] = true
		}
	}

	for event, shapes := range w.shapes {
		for shape := range shapes {
			if !observed[event][shape] {
				t.Errorf("a made-up %s has the fields %s, which no recorded %s has. Recorded: %v",
					event, shape, event, slices.Sorted(maps.Keys(observed[event])))
			}
		}
	}

	var bundle struct {
		Hooks map[string]any `json:"hooks"`
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "managed-settings", "managed-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &bundle); err != nil {
		t.Fatal(err)
	}
	for event := range bundle.Hooks {
		if len(w.shapes[event]) == 0 {
			t.Errorf("the bundle hooks %s, and the made-up organization never sends one", event)
		}
	}
	if len(w.shapes["SessionStart"]) > 0 {
		t.Error("the made-up organization sends SessionStart, whose HTTP hook has never run in a real session")
	}
}

// No event is dated before the history's first day or on or after the day after its last.
func TestEventsStayInsideTheHistory(t *testing.T) {
	k, w := sent(t)
	first, _ := time.Parse(time.DateOnly, k.FirstDay)
	last, _ := time.Parse(time.DateOnly, k.LastDay)
	if w.earliest.Before(first) || !w.latest.Before(last.AddDate(0, 0, 1)) {
		t.Errorf("events run from %s to %s; the history is %s to %s",
			w.earliest.Format(time.RFC3339), w.latest.Format(time.RFC3339), k.FirstDay, k.LastDay)
	}
}

// The answer key says what was sent: counted again from the requests themselves, the way the
// collector stores them and the views count them, it comes out the same.
func TestAnswerKeyMatchesWhatWasSent(t *testing.T) {
	k, w := sent(t)
	got := w.key()

	if k.Totals.HookEvents != w.hooks || k.Totals.LogRecords != w.records || k.Totals.MetricPoints != w.points {
		t.Errorf("the key counts %d hook events, %d log records and %d metric points; %d, %d and %d were sent",
			k.Totals.HookEvents, k.Totals.LogRecords, k.Totals.MetricPoints, w.hooks, w.records, w.points)
	}
	if k.Totals.Sessions != got.Totals.Sessions || k.Totals.Requests != got.Totals.Requests ||
		k.Totals.Prompts != got.Totals.Prompts || !near(k.Totals.CostUSD, got.Totals.CostUSD) {
		t.Errorf("totals: the key has %+v; what was sent has %+v", k.Totals, got.Totals)
	}

	for i, p := range k.People {
		g := got.People[p.Email]
		if p.Sessions != g.Sessions || p.Prompts != g.Prompts || p.Requests != g.Requests || !near(p.CostUSD, g.CostUSD) {
			t.Errorf("%s: the key has %d sessions, %d prompts, %d requests, $%.6f; sent %d, %d, %d, $%.6f",
				p.ID, p.Sessions, p.Prompts, p.Requests, p.CostUSD, g.Sessions, g.Prompts, g.Requests, g.CostUSD)
		}
		if i > 3 && t.Failed() {
			break
		}
	}

	if len(k.Days) != len(got.Days) {
		t.Errorf("the key has %d team-days; what was sent has %d", len(k.Days), len(got.Days))
	}
	for _, d := range k.Days {
		g := got.Days[d.Day+"|"+d.Team]
		if near(d.CostUSD, g.CostUSD) {
			g.CostUSD = d.CostUSD // the key rounds each day to the micro-dollar
		}
		if d != g {
			t.Errorf("%s %s: the key has %+v; what was sent has %+v", d.Day, d.Team, d, g)
		}
	}

	if len(k.Weeks) != len(got.Weeks) {
		t.Fatalf("the key has %d weeks; what was sent has %d", len(k.Weeks), len(got.Weeks))
	}
	for _, kw := range k.Weeks {
		gw := got.Weeks[kw.Week]
		if kw.ActivePeople != gw.ActivePeople || kw.Sessions != gw.Sessions || !near(kw.CostUSD, gw.CostUSD) {
			t.Errorf("week of %s: the key has %d people, %d sessions, $%.2f; sent %d, %d, $%.2f", kw.Week,
				kw.ActivePeople, kw.Sessions, kw.CostUSD, gw.ActivePeople, gw.Sessions, gw.CostUSD)
		}
		if !slices.Equal(kw.Artifacts, gw.Artifacts) {
			t.Errorf("week of %s: the key's artifacts differ from what was sent.\n key: %+v\nsent: %+v",
				kw.Week, kw.Artifacts, gw.Artifacts)
		}
		if !slices.Equal(kw.Other, gw.Other) {
			t.Errorf("week of %s: the key's names under the minimum add up differently from what was "+
				"sent.\n key: %+v\nsent: %+v", kw.Week, kw.Other, gw.Other)
		}
	}
}

// The organization is uneven in the ways that decide whether a chart misleads: spend and session
// counts each spread widely across people, and do not move together.
func TestPeopleSpreadUnevenly(t *testing.T) {
	if raceDetector {
		t.Skip("the review preset takes minutes to generate under the race detector (see storyPresets), " +
			"and this test has no goroutines of its own for it to watch")
	}
	k := keyOf(t, "review")
	months := 84 / 30.4
	var spend, sessions []float64
	for _, p := range k.People {
		spend = append(spend, p.CostUSD/months)
		sessions = append(sessions, float64(p.Sessions)/months)
	}
	if lo, hi := slices.Min(spend), slices.Max(spend); lo > 200 || hi < 2000 {
		t.Errorf("monthly spend runs from $%.0f to $%.0f; it should reach below $200 and above $2,000", lo, hi)
	}
	if lo, hi := slices.Min(sessions), slices.Max(sessions); lo > 20 || hi < 1000 {
		t.Errorf("monthly sessions run from %.0f to %.0f; they should reach below 20 and above 1,000", lo, hi)
	}
	if rho := spearman(spend, sessions); math.Abs(rho) > 0.4 {
		t.Errorf("spend and sessions are rank-correlated at %.2f; they should not move together", rho)
	}
	for _, p := range k.People {
		if !strings.HasSuffix(p.Email, "@example.com") {
			t.Errorf("%s has the address %s", p.ID, p.Email)
		}
	}
}

func TestStartIsAMonday(t *testing.T) {
	if _, err := madeup.NewOrg(small, 1, start.AddDate(0, 0, 1), 2); err == nil {
		t.Error("a history starting on a Tuesday was accepted; its weeks would not be the views' weeks")
	}
	if _, err := madeup.NewOrg(small, 1, start, 3); err == nil {
		t.Error("a two-week preset accepted three weeks")
	}
}

// ---- Reading the requests back -----------------------------------------------------------

type discard struct{}

func (discard) Hook(time.Time, []byte) error { return nil }
func (discard) Logs([]byte) error            { return nil }
func (discard) Metrics([]byte) error         { return nil }

var orgPattern = regexp.MustCompile(madeup.OrgArtifacts)

type wireSession struct {
	email, cohort string
	first         time.Time
	requests      int
	cost          float64

	mainRequests                  int
	firstMain                     time.Time
	startContext                  int
	editProposals, editRejections int
	compactions, autoCompactions  int
}

// A permission prompt's start, or a person's answer to one, as silver_policy_decision pairs them.
type wirePermission struct {
	session, prompt, tool string
	at                    time.Time
}

type wireUse struct {
	session, prompt, kind, name, fileKind string
	at                                    time.Time
	builtin                               bool
}

// wireSink decodes every request as it arrives and keeps what the views would count.
type wireSink struct {
	mu                     sync.Mutex
	err                    error
	hooks, records, points int
	hooksWithEmail         int
	emails, otherEmails    map[string]bool
	shapes                 map[string]map[string]bool
	sessions               map[string]*wireSession
	prompts                map[[2]string]bool // session, prompt: true if a built-in command
	uses                   []wireUse
	asked, answered        []wirePermission
	earliest, latest       time.Time
}

func newWireSink() *wireSink {
	return &wireSink{
		emails: map[string]bool{}, otherEmails: map[string]bool{}, shapes: map[string]map[string]bool{},
		sessions: map[string]*wireSession{}, prompts: map[[2]string]bool{},
	}
}

func (w *wireSink) session(id string) *wireSession {
	s := w.sessions[id]
	if s == nil {
		s = &wireSession{}
		w.sessions[id] = s
	}
	return s
}

func (w *wireSink) saw(at time.Time) {
	if w.earliest.IsZero() || at.Before(w.earliest) {
		w.earliest = at
	}
	if at.After(w.latest) {
		w.latest = at
	}
}

func (w *wireSink) Hook(at time.Time, body []byte) error {
	var h map[string]any
	if err := json.Unmarshal(body, &h); err != nil {
		return err
	}
	keys := strings.Join(keyOrder(nil, body), ",")
	str := func(k string) string { s, _ := h[k].(string); return s }
	event, sid, pid := str("hook_event_name"), str("session_id"), str("prompt_id")

	w.mu.Lock()
	defer w.mu.Unlock()
	w.hooks++
	w.saw(at)
	if emailShaped.Match(body) {
		w.hooksWithEmail++
	}
	if w.shapes[event] == nil {
		w.shapes[event] = map[string]bool{}
	}
	w.shapes[event][keys] = true
	switch event {
	case "InstructionsLoaded":
		// What the collector derives from file_path, before it drops it.
		path := str("file_path")
		base := path[strings.LastIndexAny(path, `/\`)+1:]
		kind := "other"
		switch {
		case base == "CLAUDE.md":
			kind = "CLAUDE.md"
		case regexp.MustCompile(`[/\\]\.claude[/\\]rules[/\\]`).MatchString(path):
			kind = "rule"
		}
		name := ""
		if orgPattern.MatchString(base) {
			name = base
		}
		w.uses = append(w.uses, wireUse{sid, pid, "instructions", name, kind, at, false})
	case "SubagentStart":
		w.uses = append(w.uses, wireUse{sid, pid, "subagent", str("agent_type"), "", at,
			slices.Contains([]string{"general-purpose", "Explore", "Plan", "statusline-setup", "claude-code-guide"}, str("agent_type"))})
	case "UserPromptExpansion":
		w.uses = append(w.uses, wireUse{sid, pid, "skill", str("command_name"), "", at, str("command_source") == "builtin"})
	case "UserPromptSubmit":
		if _, ok := w.prompts[[2]string{sid, pid}]; !ok {
			w.prompts[[2]string{sid, pid}] = false
		}
	case "PermissionRequest":
		if tool := str("tool_name"); strings.HasPrefix(tool, "mcp__") {
			w.uses = append(w.uses, wireUse{sid, pid, "mcp", strings.Split(tool, "__")[1], "", at, false})
		}
	}
	return nil
}

func (w *wireSink) Logs(body []byte) error {
	var req struct {
		ResourceLogs []struct {
			Resource  struct{ Attributes []otlpKV }
			ScopeLogs []struct {
				LogRecords []struct {
					TimeUnixNano string
					Attributes   []otlpKV
				}
			}
		}
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, rl := range req.ResourceLogs {
		res := otlpMap(rl.Resource.Attributes)
		w.noteEmails(res, false)
		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				w.records++
				a := otlpMap(lr.Attributes)
				w.noteEmails(a, true)
				ns, _ := strconv.ParseInt(lr.TimeUnixNano, 10, 64)
				at := time.Unix(0, ns).UTC()
				w.saw(at)
				sid, pid := a["session.id"], a["prompt.id"]
				s := w.session(sid)
				s.email, s.cohort = a["user.email"], res["cardo.cohort"]
				if s.first.IsZero() || at.Before(s.first) {
					s.first = at
				}
				num := func(k string) int { n, _ := strconv.Atoi(a[k]); return n }
				switch a["event.name"] {
				case "api_request":
					s.requests++
					c, _ := strconv.ParseFloat(a["cost_usd"], 64)
					s.cost += c
					if a["skill.name"] != "" {
						w.uses = append(w.uses, wireUse{sid, pid, "skill", a["skill.name"], "", at, false})
					}
					if a["query_source"] == "repl_main_thread" {
						s.mainRequests++
						if s.firstMain.IsZero() || at.Before(s.firstMain) {
							s.firstMain = at
							s.startContext = num("input_tokens") + num("cache_read_tokens") + num("cache_creation_tokens")
						}
					}
				case "user_prompt":
					w.prompts[[2]string{sid, pid}] = a["command_source"] == "builtin"
				case "tool_decision":
					tool := a["tool_name"]
					if strings.HasPrefix(tool, "mcp__") {
						w.uses = append(w.uses, wireUse{sid, pid, "mcp", strings.Split(tool, "__")[1], "", at, false})
					}
					if tool == "Edit" || tool == "Write" {
						s.editProposals++
						if a["decision"] == "reject" {
							s.editRejections++
						}
					}
					if strings.HasPrefix(a["source"], "user") {
						w.answered = append(w.answered, wirePermission{sid, pid, tool, at})
					}
				case "hook_execution_start":
					if a["hook_event"] == "PermissionRequest" {
						tool := strings.TrimPrefix(a["hook_name"], "PermissionRequest:")
						w.asked = append(w.asked, wirePermission{sid, pid, tool, at})
					}
				case "compaction":
					s.compactions++
					if a["trigger"] == "auto" {
						s.autoCompactions++
					}
				}
			}
		}
	}
	return nil
}

func (w *wireSink) Metrics(body []byte) error {
	var req struct {
		ResourceMetrics []struct {
			Resource     struct{ Attributes []otlpKV }
			ScopeMetrics []struct {
				Metrics []struct {
					Sum struct {
						DataPoints []struct{ Attributes []otlpKV }
					}
				}
			}
		}
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, rm := range req.ResourceMetrics {
		w.noteEmails(otlpMap(rm.Resource.Attributes), false)
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				for _, dp := range m.Sum.DataPoints {
					w.points++
					w.noteEmails(otlpMap(dp.Attributes), true)
				}
			}
		}
	}
	return nil
}

func (w *wireSink) noteEmails(a map[string]string, onRecord bool) {
	for k, v := range a {
		if k == "vcs.repository.url.full" {
			// An SSH remote, git@git.example.com:team/repo.git, is shaped like an address. The
			// collector reduces the URL to cardo.repo and drops it before its tripwire runs.
			continue
		}
		for _, e := range emailShaped.FindAllString(v, -1) {
			if onRecord && k == "user.email" {
				w.emails[e] = true
			} else {
				w.otherEmails[e] = true
			}
		}
	}
}

type otlpKV struct {
	Key   string
	Value struct{ StringValue string }
}

func otlpMap(kvs []otlpKV) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.StringValue
	}
	return m
}

type wireKeyView struct {
	Totals madeup.KeyTotals
	People map[string]madeup.KeyPerson
	Days   map[string]madeup.KeyDay
	Weeks  map[string]madeup.KeyWeek
}

// key counts what was sent the way the views do: see madeup.Key.
func (w *wireSink) key() wireKeyView {
	v := wireKeyView{People: map[string]madeup.KeyPerson{}, Days: map[string]madeup.KeyDay{}, Weeks: map[string]madeup.KeyWeek{}}
	type set = map[string]bool
	dayPeople, weekPeople := map[string]set{}, map[string]set{}
	contexts := map[string][]int{}
	add := func(m map[string]set, k, v string) {
		if m[k] == nil {
			m[k] = set{}
		}
		m[k][v] = true
	}
	for _, s := range w.sessions {
		day := s.first.Format(time.DateOnly)
		week := monday(s.first)
		p := v.People[s.email]
		p.Sessions++
		p.Requests += s.requests
		p.CostUSD += s.cost
		v.People[s.email] = p
		d := v.Days[day+"|"+s.cohort]
		d.Day, d.Team = day, s.cohort
		d.Sessions++
		d.CostUSD += s.cost
		d.EditProposals += s.editProposals
		d.EditRejections += s.editRejections
		d.Compactions += s.compactions
		d.AutoCompactions += s.autoCompactions
		if s.mainRequests > 0 {
			d.SessionsWithRequests++
			contexts[day+"|"+s.cohort] = append(contexts[day+"|"+s.cohort], s.startContext)
		}
		v.Days[day+"|"+s.cohort] = d
		add(dayPeople, day+"|"+s.cohort, s.email)
		wk := v.Weeks[week]
		wk.Week = week
		wk.Sessions++
		wk.CostUSD += s.cost
		v.Weeks[week] = wk
		add(weekPeople, week, s.email)
		v.Totals.Sessions++
		v.Totals.Requests += s.requests
		v.Totals.CostUSD += s.cost
	}
	for sp, builtin := range w.prompts {
		if builtin {
			continue
		}
		s := w.sessions[sp[0]]
		p := v.People[s.email]
		p.Prompts++
		v.People[s.email] = p
		k := s.first.Format(time.DateOnly) + "|" + s.cohort
		d := v.Days[k]
		d.Prompts++
		v.Days[k] = d
		v.Totals.Prompts++
	}
	for k, people := range dayPeople {
		d := v.Days[k]
		d.People = len(people)
		if c := contexts[k]; len(c) > 0 {
			slices.Sort(c)
			d.StartContextP50 = c[min(len(c)-1, len(c)/2)]
		}
		v.Days[k] = d
	}

	// Each permission prompt is answered by the first answer at or after it, for the same
	// session, prompt and tool, as the view's ASOF join takes it.
	answers := map[[3]string][]time.Time{}
	for _, a := range w.answered {
		k := [3]string{a.session, a.prompt, a.tool}
		answers[k] = append(answers[k], a.at)
	}
	for _, ts := range answers {
		slices.SortFunc(ts, func(a, b time.Time) int { return a.Compare(b) })
	}
	for _, q := range w.asked {
		s := w.sessions[q.session]
		k := s.first.Format(time.DateOnly) + "|" + s.cohort
		d := v.Days[k]
		d.PermissionPrompts++
		ts := answers[[3]string{q.session, q.prompt, q.tool}]
		if i, _ := slices.BinarySearchFunc(ts, q.at, func(a, b time.Time) int { return a.Compare(b) }); i < len(ts) {
			d.PermissionWaitMs += ts[i].UnixMilli() - q.at.UnixMilli()
		}
		v.Days[k] = d
	}

	// Commands, skills and MCP servers count once per prompt, at their earliest use.
	type perPrompt struct{ session, prompt, kind, name string }
	first := map[perPrompt]wireUse{}
	var rows []wireUse
	for _, u := range w.uses {
		if u.kind == "skill" || u.kind == "mcp" {
			k := perPrompt{u.session, u.prompt, u.kind, u.name}
			if f, ok := first[k]; ok {
				u.at = minTime(u.at, f.at)
				u.builtin = u.builtin || f.builtin
			}
			first[k] = u
			continue
		}
		rows = append(rows, u)
	}
	for _, u := range first {
		rows = append(rows, u)
	}
	type artKey struct{ week, kind, name, fileKind string }
	type artCount struct {
		builtin          bool
		people, sessions set
		uses             int
	}
	arts := map[artKey]*artCount{}
	for _, u := range rows {
		k := artKey{monday(u.at), u.kind, u.name, u.fileKind}
		a := arts[k]
		if a == nil {
			a = &artCount{people: set{}, sessions: set{}}
			arts[k] = a
		}
		a.builtin = a.builtin || u.builtin
		a.people[w.sessions[u.session].email] = true
		a.sessions[u.session] = true
		a.uses++
	}
	for k, a := range arts {
		org := k.name != "" && (k.kind == "instructions" || orgPattern.MatchString(k.name) ||
			(k.kind == "mcp" && orgPattern.MatchString("mcp__"+k.name+"__")))
		wk := v.Weeks[k.week]
		wk.Week = k.week
		wk.Artifacts = append(wk.Artifacts, madeup.KeyArtifact{
			Kind: k.kind, Name: k.name, FileKind: k.fileKind, Org: org, Builtin: a.builtin,
			People: len(a.people), Sessions: len(a.sessions), Uses: a.uses,
		})
		v.Weeks[k.week] = wk
	}
	// The names fewer than five people used in a week, added up per kind with each person and
	// session counted once.
	other := map[[2]string]*struct {
		people, sessions set
		uses             int
	}{}
	for k, a := range arts {
		org := k.name != "" && (k.kind == "instructions" || orgPattern.MatchString(k.name))
		if org || a.builtin || k.name == "" || len(a.people) >= madeup.MinGroupSize {
			continue
		}
		o := other[[2]string{k.week, k.kind}]
		if o == nil {
			o = &struct {
				people, sessions set
				uses             int
			}{people: set{}, sessions: set{}}
			other[[2]string{k.week, k.kind}] = o
		}
		maps.Copy(o.people, a.people)
		maps.Copy(o.sessions, a.sessions)
		o.uses += a.uses
	}
	for wk, o := range other {
		w := v.Weeks[wk[0]]
		w.Other = append(w.Other, madeup.KeyArtifact{
			Kind: wk[1], Name: "other", People: len(o.people), Sessions: len(o.sessions), Uses: o.uses,
		})
		v.Weeks[wk[0]] = w
	}
	for week, wk := range v.Weeks {
		slices.SortFunc(wk.Other, func(a, b madeup.KeyArtifact) int { return cmp.Compare(a.Kind, b.Kind) })
		wk.ActivePeople = len(weekPeople[week])
		sort.Slice(wk.Artifacts, func(i, j int) bool {
			a, b := wk.Artifacts[i], wk.Artifacts[j]
			return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(b.People, a.People),
				cmp.Compare(a.Name, b.Name), cmp.Compare(a.FileKind, b.FileKind)) < 0
		})
		v.Weeks[week] = wk
	}
	return v
}

func monday(t time.Time) string {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)).Format(time.DateOnly)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-5+1e-9*math.Abs(a) }

// keyOrder is a JSON object's keys, in the order they appear.
func keyOrder(t *testing.T, b []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil && t != nil {
		t.Fatal(err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			if t != nil {
				t.Fatal(err)
			}
			return keys
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil && t != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func spearman(a, b []float64) float64 {
	rank := func(v []float64) []float64 {
		idx := make([]int, len(v))
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(i, j int) bool { return v[idx[i]] < v[idx[j]] })
		r := make([]float64, len(v))
		for pos, i := range idx {
			r[i] = float64(pos)
		}
		return r
	}
	ra, rb := rank(a), rank(b)
	n := float64(len(a))
	d2 := 0.0
	for i := range ra {
		d2 += (ra[i] - rb[i]) * (ra[i] - rb[i])
	}
	return 1 - 6*d2/(n*(n*n-1))
}
