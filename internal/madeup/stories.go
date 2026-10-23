package madeup

import (
	"fmt"
	"math/rand/v2"
	"slices"
)

// The stories: patterns put into the made-up organization on purpose, each aimed at a view
// (ADR-0051, decision 5). A view is done once it has been seen showing its story. A story no
// view shows is a gap in Cardo, and stays on the list below until a view shows it.
//
// Every effect here is planted, so a chart that shows one shows only that the generator was told
// to put it in. The sizes (30% less, a fifth more, about three times) are placeholders, chosen
// to be large enough to see.
//
// A story changes what a session costs, never how much work it does. A session's requests are
// planned from the person's month as if no story touched it, at their own model and without the
// story's extra context; the session is then sent with the story's model, context or bug, and
// costs what that costs. So a team that moves to a dearer model spends more for the same work, as
// a real one would.

// Stories is when each story happens in a preset and which teams play which part. Weeks count
// from 1, the first week of the history. A week of 0 leaves the story out.
type Stories struct {
	// DeployCheck is how /deploy-check, a command one engineer wrote, spreads: each week, how many
	// more people in which team start using it.
	DeployCheck []Spread
	// The week the organization ships example-standards-v3.md, which every session in its
	// repositories loads; the teams in StayOnV2 keep loading v2.
	InstructionsV3 int
	StayOnV2       []string
	// The week /review, the organization's own command, is rolled out to every team but NoReview.
	Review   int
	NoReview []string
	// The week the Jira connector and the api-guidelines skill are set up for everyone.
	Connector int
	// The week one team makes the most expensive model its default.
	ExpensiveModel int
	ExpensiveTeam  string
	// The weeks whose Claude Code releases have a caching bug. The next week's first release
	// fixes it, and people upgrade at their own pace, as always.
	Bug [2]int
	// The week perf-budget, a skill of the organization's, is rolled out to two teams: the week
	// the caching bug arrives, so a before-and-after for those teams sees two changes at once.
	PerfBudget      int
	PerfBudgetTeams []string
	// The team whose test command fails now and then, so its sessions run it again and again.
	FlakyTeam string
	// The week someone in GamerTeam starts running /review from a script, dozens of times a day,
	// in sessions that do nothing else.
	Gamer     int
	GamerTeam string
	// How many people leave a long loop running overnight, a few nights a week.
	Overnight int
}

// Spread is a step in an artifact's spread: in Week, People more people in Team start using it.
type Spread struct {
	Week   int
	Team   string
	People int
}

// The planted effects. Placeholders, like the calibration.
const (
	// In a session that used /deploy-check, Edit and Write are this much likelier among the
	// tools a request ends in, so the same spend buys more accepted edits: about 30% less cost
	// per accepted edit.
	deployCheckEditBoost = 2.0
	// The chance a session of someone using /deploy-check uses it, once they have used it that
	// week. The first session of each week they work always does.
	deployCheckChance = 0.3
	// The chance a session in a team /review was rolled out to uses it. Sessions that use it are
	// no different from those that do not.
	reviewChance = 0.2
	// What example-standards-v3.md adds to every session that loads it, beyond v2's 5,000
	// tokens: about a fifth more context at the start of a session.
	instructionsV3Tokens = 6000
	// The Jira connector's tools, in every session's context once it is set up.
	connectorTokens = 2500
	// How many people call the Jira connector each week, once it is set up for everyone.
	jiraCallersPerWeek = 3
	// The chance, per prompt, that the model uses api-guidelines once it is set up: rarely.
	apiGuidelinesChance = 0.0004
	// The chance, per prompt in the teams it was rolled out to, that the model uses perf-budget.
	// It changes nothing about the session.
	perfBudgetChance = 0.06
	// The chance a request on a release with the caching bug finds the cache cold, apart from
	// Claude Code's own prefix, and writes the conversation again.
	bugColdChance = 0.10
	// The chance a prompt in the flaky team runs into the failing test command. The command is
	// then run 3 to 7 times, the model reading each failure, until it passes.
	flakyChance = 0.12
	// The model the expensive-default team moves to.
	expensiveModel = "claude-fable-5-1"
	// How many /review sessions the gaming script runs on a working day.
	gamerSessionsMin, gamerSessionsMax = 60, 100
	// A Claude Code release every two days.
	releaseEveryDays = 2
)

// Story is one entry on the list of stories: what was planted, where it should show, and what no
// view shows yet. It goes into the answer key with the figures the generator measured.
type Story struct {
	Name    string
	Planted string
	// Shows names the dashboard panel that shows it. Empty when no panel does.
	Shows string
	// Gap says what no view shows. Empty when a view shows all of it.
	Gap string
}

// catalog is the list of stories, in the order the answer key and the docs give them.
var catalog = []Story{
	{
		Name: "deploy-check-spreads",
		Planted: "/deploy-check, a command one engineer wrote, starts in one team in week 2. It crosses five " +
			"people in week 5 and reaches four teams by week 12. Each person using it uses it every week they work.",
		Shows: "Enablement, \"Built by engineers, used by several\": \"other\" until week 5, then named, " +
			"with its peak weekly people.",
		Gap: "No view says which teams use an artifact, so its spread from one team to four cannot be " +
			"seen, and spread across teams is the vote for what teams chose (ADR-0040).",
	},
	{
		Name: "instructions-v3",
		Planted: "The organization ships example-standards-v3.md in week 4, about a fifth more context " +
			"for every session that loads it. Two teams keep loading v2.",
		Shows: "Enablement, \"Versions in use\" and \"Sessions on a stale version\": v2 stays in use. " +
			"\"Context a session starts from\": larger from week 4 in the teams on v3.",
	},
	{
		Name: "review-rollout",
		Planted: "/review, the organization's own command, is rolled out in week 6 to every team but two " +
			"small ones. Sessions that use it are no different from those that do not.",
		Shows: "Enablement, \"Weekly reach of the organization's artifacts\": /review rises in week 6. " +
			"Whether it helps is the cost-per-edit story.",
	},
	{
		Name: "connector-rarely-called",
		Planted: "A Jira connector is set up for everyone in week 6. Its tools are in every session's " +
			"context from then on, and about three people call it each week.",
		Shows: "Enablement, \"Weekly reach of the organization's artifacts\": jira's reach stays at a few " +
			"people. \"Context a session starts from\": every session carries its tools.",
	},
	{
		Name:    "skill-rarely-triggers",
		Planted: "The api-guidelines skill is set up for everyone in week 6, and the model rarely uses it.",
		Shows:   "Enablement, \"Weekly reach of the organization's artifacts\": a few people a week.",
	},
	{
		Name: "flaky-tests",
		Planted: "One team's test command fails now and then. Sessions run it 3 to 7 times in a row, the " +
			"model reading each failure, until it passes.",
		Gap: "No view shows failing tool calls, or a command run again and again: silver_tool_call " +
			"has whether each call succeeded, and no gold view reads it.",
	},
	{
		Name: "compactions-by-style",
		Planted: "Compactions per session differ widely by way of working: dozens in a long autonomous " +
			"run, almost none in a session that asks one quick question.",
		Shows: "Enablement, \"Compactions per session, with sessions\": the fleet's average, day by day.",
		Gap: "One average hides that a few long sessions make most of the compactions. No view shows " +
			"how compactions spread across sessions.",
	},
	{
		Name: "permission-waits",
		Planted: "People in the default permission mode answer prompts for edits and commands; people in " +
			"auto mode answer none.",
		Shows: "Enablement, \"Permission waits, with the prompts answered\".",
	},
	{
		Name: "heavy-tail",
		Planted: "A few people spend much of the money: monthly spend runs from about $100 to $4,000, " +
			"and does not follow session counts.",
		Gap: "By design no view shows one person's spend to anyone else (the per-person rule, INV-3). " +
			"The view of cost over a published budget (ADR-0033) is not built, and the fleet " +
			"dashboard's spend per engineer reads the cost data this stack does not have yet.",
	},
	{
		Name: "expensive-default",
		Planted: "One team makes the most expensive model its default in week 8, and its spend for the " +
			"same work rises about three times.",
		Shows: "Enablement, \"Daily spend by cohort\": the team's spend rises from week 8.",
		Gap: "The collector's views have no spend by model, so the chart shows that the spend rose, not " +
			"why. The fleet dashboard's \"Model mix by spend\" reads the cost data this stack does not " +
			"have yet.",
	},
	{
		Name: "caching-bug",
		Planted: "Claude Code releases in weeks 10 and 11 have a caching bug that raises cost per request. " +
			"A release early in week 12 fixes it, and people upgrade at their own pace.",
		Gap: "No view shows cost per request by Claude Code version. \"Daily spend by cohort\" rises " +
			"everywhere, with nothing to say why.",
	},
	{
		Name: "overnight-loops",
		Planted: "Two people leave a long loop running overnight, a few nights a week, for hours with " +
			"nobody at the keyboard.",
		Gap: "No view shows sessions that run while nobody is there, or what they cost.",
	},
	{
		Name: "cost-per-edit",
		Planted: "Sessions that use /deploy-check cost about 30% less per accepted edit than the same " +
			"people's other sessions. Sessions that use /review cost the same as those that do not.",
		Gap: "No view compares sessions that used an artifact with sessions that did not, so neither " +
			"the difference nor its absence can be seen.",
	},
	{
		Name: "privacy-floor",
		Planted: "A three-person team, a command four people use, and personal commands, subagents and " +
			"connectors no more than two people use.",
		Shows: "Every view: the team is always \"other\", and so are the names, every week " +
			"(the five-person minimum, ADR-0029).",
	},
	{
		Name: "usage-gaming",
		Planted: "From week 7 one person runs /review from a script, dozens of times a working day, in " +
			"sessions that do nothing else.",
		Shows: "Enablement, \"The organization's artifacts in use\": /review's sessions and uses jump " +
			"in week 7, and its peak weekly people do not.",
	},
	{
		Name: "two-changes-at-once",
		Planted: "perf-budget, a skill of the organization's, is rolled out to two teams in week 10, the " +
			"week the caching bug arrives. It changes nothing, and those teams' cost per request rises.",
		Gap: "No view sets a before against an after, so nothing warns that two things changed in the " +
			"same week, and a reader credits the skill with the bug.",
	},
}

// assignStories gives people their parts: who picks up /deploy-check and when, who calls the
// Jira connector each week, who runs overnight loops, and who games /review.
func assignStories(r *rand.Rand, o *Org) {
	st := o.Preset.Stories
	weeks := o.Days / 7
	byTeam := map[string][]*Person{}
	for _, p := range o.People {
		p.JiraWeeks = map[int]bool{}
		byTeam[p.Team.Name] = append(byTeam[p.Team.Name], p)
	}
	// Someone who types commands, and works most days, is the kind of person who takes one up.
	keen := func(p *Person) bool { return !p.Style.Scripted && p.Style.Name != "occasional" }
	shuffled := func(ps []*Person) []*Person {
		out := slices.Clone(ps)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		// The keen first, so a team runs out of them before anyone else is taken.
		slices.SortStableFunc(out, func(a, b *Person) int {
			switch ka, kb := keen(a), keen(b); {
			case ka && !kb:
				return -1
			case kb && !ka:
				return 1
			}
			return 0
		})
		return out
	}

	pools := map[string][]*Person{}
	for _, step := range st.DeployCheck {
		if step.Week > weeks {
			continue
		}
		if pools[step.Team] == nil {
			for _, p := range shuffled(byTeam[step.Team]) {
				if !p.Style.Scripted {
					pools[step.Team] = append(pools[step.Team], p)
				}
			}
		}
		for range step.People {
			if len(pools[step.Team]) == 0 {
				break
			}
			pools[step.Team][0].DeployCheckWeek = step.Week
			pools[step.Team] = pools[step.Team][1:]
		}
	}

	if st.Connector > 0 {
		var callers []*Person
		for _, p := range o.People {
			if !p.Style.Scripted {
				callers = append(callers, p)
			}
		}
		for w := st.Connector; w <= weeks; w++ {
			order := slices.Clone(callers)
			r.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			for _, p := range order[:min(jiraCallersPerWeek, len(order))] {
				p.JiraWeeks[w] = true
			}
		}
	}

	// Overnight loops are left running by people in auto mode, whose sessions never stop to ask.
	night := 0
	for _, p := range shuffled(o.People) {
		if night == st.Overnight {
			break
		}
		if p.Mode == "auto" && !p.Style.Scripted && (p.Style.Name == "long-runs" || p.Style.Name == "parallel") {
			p.Overnight = true
			night++
		}
	}
	for _, p := range shuffled(o.People) {
		if night == st.Overnight {
			break
		}
		if p.Mode == "auto" && !p.Style.Scripted && !p.Overnight {
			p.Overnight = true
			night++
		}
	}

	if st.Gamer > 0 && st.Gamer <= weeks {
		for _, p := range shuffled(byTeam[st.GamerTeam]) {
			if keen(p) && !p.Overnight {
				p.Gamer = true
				break
			}
		}
	}
}

// release is the Claude Code release a person runs on a day: one every releaseEveryDays,
// reaching them after their own lag. Releases count from 1 on the history's first day.
func (o *Org) release(p *Person, day int) int {
	return 1 + max(0, day-p.UpgradeLag)/releaseEveryDays
}

// version is how a release names itself. The numbers are made up, and say so: a chart of cost by
// version must never read as a finding about a real Claude Code release.
func version(release int) string { return fmt.Sprintf("9.0.%d-madeup", release) }

// buggy says whether a release has the caching bug: it came out in one of the bug's weeks.
func (o *Org) buggy(release int) bool {
	b := o.Preset.Stories.Bug
	if b[0] == 0 {
		return false
	}
	day := (release - 1) * releaseEveryDays
	return day >= 7*(b[0]-1) && day < 7*b[1]
}

// onTeam says whether a team is in a story's list.
func onTeam(teams []string, team string) bool { return slices.Contains(teams, team) }

// Catalog is the list of stories, in order: the gap list is the ones with a Gap.
func Catalog() []Story { return slices.Clone(catalog) }
