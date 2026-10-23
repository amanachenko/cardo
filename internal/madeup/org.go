package madeup

import (
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"time"
)

// OrgArtifacts is the made-up organization's CARDO_ORG_ARTIFACTS: the names of what it ships
// itself. docker-compose.made-up.yml gives the collector and the views the same pattern, and
// test/madeup_test.go holds the two together.
const OrgArtifacts = `^(review|onboarding|test-fixer|jira|api-guidelines|perf-budget|example-standards-v[0-9]+[.]md)$`

// OrgRepos is its CARDO_ORG_REPOS: every repository on its own git host.
const OrgRepos = `^git[.]example[.]com/`

var orgPattern = regexp.MustCompile(OrgArtifacts)

// organizationID is what every made-up session sends as organization.id.
const organizationID = "00000000-0000-4000-8000-00000000e0e0"

// Preset is a made-up organization's size: how many people, in which teams, over how many weeks,
// and when its stories happen.
type Preset struct {
	Name    string
	Weeks   int
	Teams   []TeamSpec
	Stories Stories
}

// TeamSpec is one team: its name, which is also its cohort label, how many people it has, and its
// hours' offset from UTC.
type TeamSpec struct {
	Name   string
	People int
	UTC    float64
	Repos  []string
}

// Presets by name. The review preset is the one people look at.
var Presets = map[string]Preset{
	"review": {
		Name:  "review",
		Weeks: 12,
		Teams: []TeamSpec{
			{"payments", 18, 1, []string{"payments-api", "ledger"}},
			{"platform", 14, 0, []string{"platform-infra", "build-tools"}},
			{"mobile", 11, -5, []string{"mobile-app", "mobile-sdk"}},
			{"data", 8, 2, []string{"pipelines", "warehouse"}},
			{"web", 6, -8, []string{"web-frontend", "design-system"}},
			{"security", 3, 1, []string{"secrets-scanner"}},
		},
		Stories: Stories{
			DeployCheck: []Spread{
				{2, "platform", 1}, {3, "platform", 1}, {4, "platform", 2}, {5, "platform", 3},
				{6, "payments", 2}, {7, "payments", 2}, {7, "platform", 1}, {8, "data", 2},
				{9, "payments", 1}, {9, "data", 1}, {10, "mobile", 2}, {11, "mobile", 1},
				{11, "payments", 1}, {12, "data", 1}, {12, "mobile", 1},
			},
			InstructionsV3: 4, StayOnV2: []string{"mobile", "web"},
			Review: 6, NoReview: []string{"web", "security"},
			Connector:      6,
			ExpensiveModel: 8, ExpensiveTeam: "platform",
			Bug:        [2]int{10, 11},
			PerfBudget: 10, PerfBudgetTeams: []string{"payments", "data"},
			FlakyTeam: "mobile",
			Gamer:     7, GamerTeam: "payments",
			Overnight: 2,
		},
	},
}

// People is the preset's headcount.
func (p Preset) People() int {
	n := 0
	for _, t := range p.Teams {
		n += t.People
	}
	return n
}

// Org is one made-up organization: its people, and the dates its history covers.
type Org struct {
	Preset Preset
	Seed   uint64
	Start  time.Time // a Monday, 00:00 UTC
	Days   int
	People []*Person
}

// End is the first moment after the history: no event is at or after it.
func (o *Org) End() time.Time { return o.Start.AddDate(0, 0, o.Days) }

// Person is one made-up engineer. Nothing about them comes from a real person.
type Person struct {
	Index    int
	ID       string // engineer-007
	Email    string // engineer-007@example.com
	Team     TeamSpec
	Style    *Style
	OS       string // darwin, linux or windows
	Terminal string
	Model    string
	Effort   string
	Mode     string

	// The person's placeholder month, from their style's ranges.
	MonthlySpend    float64
	MonthlySessions float64

	JoinDay    int          // the first day they use Claude Code
	DaysOff    map[int]bool // holidays
	UpgradeLag int          // days between a Claude Code release and their upgrade
	BashAllow  float64      // the share of their Bash calls an allow rule already covers

	UserClaudeMD bool
	Commands     []string // commands they use, besides the built-in and the organization's
	Subagents    []string // subagents they start, besides Claude Code's own
	MCPServers   []string // MCP servers they have connected
	Repos        []Repo

	// Their parts in the stories (stories.go).
	DeployCheckWeek int          // the week they start using /deploy-check; 0 if they never do
	JiraWeeks       map[int]bool // the weeks they call the Jira connector
	Overnight       bool         // leaves a long loop running overnight, a few nights a week
	Gamer           bool         // runs /review from a script, dozens of times a working day

	// Identifiers Claude Code sends beside the email address, which the collector removes.
	deviceID, accountUUID string
	stream                uint64
}

// Repo is a repository a person works in. URL is what Claude Code would send as
// vcs.repository.url.full.
type Repo struct {
	Name string
	URL  string
	Org  bool
}

// Home-grown artifacts. Each shared one is used by a fixed number of people; each personal one
// by no more than two, so the views must show it as "other" (ADR-0029).
var (
	sharedCommands = []struct {
		Name   string
		People int
	}{
		{"pr-summary", 9},
		{"db-migrate", 4}, // one short of the five-person minimum
	}
	personalCommands = []string{
		"standup", "scratch", "explain-diff", "todo-sweep", "fix-lint", "my-review", "release-notes",
		"bench", "changelog", "pair", "tidy-imports", "spike", "retro-notes", "perf-check",
		"api-docs", "triage", "rename-all", "deps-bump", "story-split", "log-dive",
	}
	sharedMCP = []struct {
		Name   string
		People int
	}{
		{"github", 18},
		{"sentry", 7},
	}
	personalMCP       = []string{"notes", "local-db", "figma-personal", "calendar"}
	frameworkAgents   = []string{"architect", "implementer", "verifier"}
	personalSubagents = []string{"my-researcher", "sql-helper", "doc-writer"}
	builtinSubagents  = []weighted{{"Explore", 0.6}, {"general-purpose", 0.3}, {"Plan", 0.1}}
	externalRepos     = []string{"parser", "http-client", "ui-kit"}
)

// NewOrg builds the made-up organization for a preset, a seed and a start date. The same three
// always give the same organization.
func NewOrg(p Preset, seed uint64, start time.Time, weeks int) (*Org, error) {
	if start.Weekday() != time.Monday || start.Hour() != 0 || start.Minute() != 0 || start.Location() != time.UTC {
		return nil, fmt.Errorf("the start must be a Monday at 00:00 UTC, not %s", start.Format(time.RFC3339))
	}
	if weeks < 1 || weeks > p.Weeks {
		return nil, fmt.Errorf("the %s preset covers 1 to %d weeks, not %d", p.Name, p.Weeks, weeks)
	}
	o := &Org{Preset: p, Seed: seed, Start: start, Days: 7 * weeks}
	r := newRand(seed, 0)
	n := p.People()

	var teams []TeamSpec
	for _, t := range p.Teams {
		for range t.People {
			teams = append(teams, t)
		}
	}
	styles := apportion(n)
	r.Shuffle(len(styles), func(i, j int) { styles[i], styles[j] = styles[j], styles[i] })

	for i := range n {
		id := fmt.Sprintf("engineer-%03d", i+1)
		o.People = append(o.People, &Person{
			Index: i, ID: id, Email: id + "@example.com",
			Team: teams[i], Style: styles[i], stream: 1<<32 | uint64(i),
		})
	}
	for _, pr := range o.People {
		pr.traits(o)
	}
	assignShared(r, o.People)
	assignStories(r, o)
	return o, nil
}

// apportion gives each style its share of n people, by largest remainder, in Styles order.
func apportion(n int) []*Style {
	total := 0
	for _, s := range Styles {
		total += s.Share
	}
	counts := make([]int, len(Styles))
	type rem struct {
		i int
		f float64
	}
	var rems []rem
	given := 0
	for i, s := range Styles {
		exact := float64(n*s.Share) / float64(total)
		counts[i] = int(exact)
		given += counts[i]
		rems = append(rems, rem{i, exact - float64(counts[i])})
	}
	slices.SortStableFunc(rems, func(a, b rem) int {
		switch {
		case a.f > b.f:
			return -1
		case a.f < b.f:
			return 1
		}
		return 0
	})
	for k := 0; given < n; k++ {
		counts[rems[k].i]++
		given++
	}
	var out []*Style
	for i, s := range Styles {
		for range counts[i] {
			out = append(out, s)
		}
	}
	return out
}

// traits draws everything about one person that does not depend on anyone else.
func (p *Person) traits(o *Org) {
	r := newRand(o.Seed, p.stream)
	s := p.Style
	p.OS = pick(r, []weighted{{"darwin", 0.55}, {"linux", 0.25}, {"windows", 0.20}})
	p.Terminal = s.Terminals[r.IntN(len(s.Terminals))]
	if p.OS == "windows" && (p.Terminal == "iTerm.app" || p.Terminal == "tmux") {
		p.Terminal = "WindowsTerminal"
	}
	if p.OS != "windows" && p.Terminal == "WindowsTerminal" {
		p.Terminal = "vscode"
	}
	p.Model = pick(r, mainModels)
	p.Effort = pick(r, effortLevels)
	p.Mode = pick(r, s.Modes)
	p.MonthlySpend = logUniform(r, s.Spend[0], s.Spend[1])
	p.MonthlySessions = logUniform(r, s.Sessions[0], s.Sessions[1])
	x := r.Float64()
	for _, l := range upgradeLags {
		if x < l.Share || l == upgradeLags[len(upgradeLags)-1] {
			p.UpgradeLag = l.Days[0] + r.IntN(l.Days[1]-l.Days[0]+1)
			break
		}
		x -= l.Share
	}
	p.BashAllow = 0.2 + 0.6*r.Float64()
	p.UserClaudeMD = r.Float64() < 0.6

	// Most people use Claude Code from the first day; some join later.
	if r.Float64() < 0.15 {
		p.JoinDay = min(o.Days-1, 7+r.IntN(49))
	}
	// Holidays: any week has a one-in-twenty chance of being one, and some run into the next, so
	// about one week in fifteen is off however long the history is.
	p.DaysOff = map[int]bool{}
	weeks := o.Days / 7
	for w := 0; w < weeks; w++ {
		if r.Float64() >= 0.05 {
			continue
		}
		off := 1
		if r.Float64() < 0.3 && w+1 < weeks {
			off = 2
		}
		for d := range 7 * off {
			p.DaysOff[7*w+d] = true
		}
	}

	for _, name := range p.Team.Repos {
		p.Repos = append(p.Repos, Repo{Name: name, Org: true, URL: orgRepoURL(r, p.Team.Name, name)})
	}
	ext := externalRepos[r.IntN(len(externalRepos))]
	p.Repos = append(p.Repos, Repo{Name: ext, URL: "https://github.com/example-oss/" + ext})

	if s.Name == "subagent-heavy" {
		p.Subagents = append(p.Subagents, frameworkAgents...)
	}
	if r.Float64() < 0.5 || s.Name == "subagent-heavy" {
		p.Subagents = append(p.Subagents, "test-fixer")
	}
	p.deviceID = fmt.Sprintf("%016x%016x%016x%016x", r.Uint64(), r.Uint64(), r.Uint64(), r.Uint64())
	p.accountUUID = uuid(r)
}

// orgRepoURL is a remote on the organization's git host, in one of the two SSH forms. The
// collector reduces every form to host/owner/name before matching it (ADR-0035), and its own
// tests send every form; these never name a host as a URL the code could be taken to call.
func orgRepoURL(r *rand.Rand, team, name string) string {
	if r.Float64() < 0.3 {
		return "ssh://git@git.example.com/" + team + "/" + name + ".git"
	}
	return "git@git.example.com:" + team + "/" + name + ".git"
}

// assignShared hands out the artifacts several people share, so each is used by exactly the
// number of people it is meant to have, and the personal ones, so none is used by more than two.
func assignShared(r *rand.Rand, people []*Person) {
	order := func() []*Person {
		out := slices.Clone(people)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	for _, c := range sharedCommands {
		for _, p := range order()[:min(c.People, len(people))] {
			p.Commands = append(p.Commands, c.Name)
		}
	}
	for _, m := range sharedMCP {
		for _, p := range order()[:min(m.People, len(people))] {
			p.MCPServers = append(p.MCPServers, m.Name)
		}
	}
	// Personal names go round in turn, two people each at most.
	next := 0
	give := func(p *Person, pool []string, to *[]string) {
		if next < 2*len(pool) {
			*to = append(*to, pool[next%len(pool)])
			next++
		}
	}
	next = 0
	for _, p := range order() {
		if r.Float64() < 0.5 {
			give(p, personalCommands, &p.Commands)
		}
	}
	next = 0
	for _, p := range order() {
		if r.Float64() < 0.1 {
			give(p, personalMCP, &p.MCPServers)
		}
	}
	next = 0
	for _, p := range order() {
		if r.Float64() < 0.08 {
			give(p, personalSubagents, &p.Subagents)
		}
	}
}

func newRand(seed, stream uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, stream)) }

func pick(r *rand.Rand, ws []weighted) string {
	total := 0.0
	for _, w := range ws {
		total += w.Weight
	}
	x := r.Float64() * total
	for _, w := range ws {
		if x < w.Weight {
			return w.Value
		}
		x -= w.Weight
	}
	return ws[len(ws)-1].Value
}

func logUniform(r *rand.Rand, lo, hi float64) float64 {
	return math.Exp(math.Log(lo) + r.Float64()*(math.Log(hi)-math.Log(lo)))
}

// logNormal draws around a median, with sigma the spread of its logarithm.
func logNormal(r *rand.Rand, median, sigma float64) float64 {
	return median * math.Exp(sigma*r.NormFloat64())
}

// poisson draws a count with the given mean.
func poisson(r *rand.Rand, mean float64) int {
	if mean <= 0 {
		return 0
	}
	if mean > 30 {
		return max(0, int(math.Round(mean+math.Sqrt(mean)*r.NormFloat64())))
	}
	l, k, p := math.Exp(-mean), 0, 1.0
	for {
		p *= r.Float64()
		if p <= l {
			return k
		}
		k++
	}
}
