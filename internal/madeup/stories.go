package madeup

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
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
	// The week the organization ships example-engineering-standards-v3.md, which every session in
	// its repositories loads; the teams in StayOnV2 keep loading v2.
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
	// What example-engineering-standards-v3.md adds to every session that loads it, beyond v2's
	// 5,000 tokens: about a fifth more context at the start of a session.
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

// catalog is the list of stories for a preset's history of weeks, in the order the answer key and
// the docs give them. The weeks and teams in each story's text are the preset's own.
func catalog(p Preset, weeks int) []Story {
	st := p.Stories
	spread := st.spread(weeks)
	small := smallTeam(p)
	db := 0
	for _, c := range sharedCommands {
		if c.Name == "db-migrate" {
			db = c.People
		}
	}
	bug := fmt.Sprintf("week %d", st.Bug[0])
	if st.Bug[1] > st.Bug[0] {
		bug = fmt.Sprintf("weeks %d and %d", st.Bug[0], st.Bug[1])
	}
	stay := fmt.Sprintf("The %s teams keep", join(st.StayOnV2))
	if len(st.StayOnV2) == 1 {
		stay = fmt.Sprintf("The %s team keeps", st.StayOnV2[0])
	}
	night := fmt.Sprintf("%s people leave", upper(count(st.Overnight)))
	if st.Overnight == 1 {
		night = "One person leaves"
	}

	return []Story{
		{
			Name: "deploy-check-spreads",
			Planted: fmt.Sprintf("/deploy-check, a command one engineer wrote, starts in %s in week %d. "+
				"It crosses five people in week %d and reaches %s by week %d. Each person using it uses it "+
				"every week they work.", teams(spread.firstTeams), spread.first, spread.named,
				teams(spread.lastTeams), weeks),
			Shows: fmt.Sprintf("Enablement, \"Brought in by engineers, used by several\": \"other\" until "+
				"week %d, then named, in %s last week, with more people than four weeks before. Spread "+
				"across teams is the vote for what teams chose (ADR-0040).", spread.named, teams(spread.lastTeams)),
		},
		{
			Name: "instructions-v3",
			Planted: fmt.Sprintf("The organization ships example-engineering-standards-v3.md in week %d, about a fifth "+
				"more context for every session that loads it. %s loading v2.", st.InstructionsV3, stay),
			Shows: fmt.Sprintf("Enablement, \"Versions in use\": v3 current and v2 older. \"Sessions on an "+
				"older version\": none until week %d, then the sessions of the %s. \"Each team on the "+
				"current version, against the fleet\": the %s at none from week %d, each from the first "+
				"week five of its people worked. \"Context every session starts with\": a step up in week "+
				"%d.", st.InstructionsV3-1, onV2(st.StayOnV2),
				onV2(st.StayOnV2), st.InstructionsV3, st.InstructionsV3),
		},
		{
			Name: "review-rollout",
			Planted: fmt.Sprintf("/review, the organization's own command, is rolled out in week %d to every "+
				"team but %s. Sessions that use it are no different from those that do not.",
				st.Review, join(st.NoReview)),
			Shows: fmt.Sprintf("Enablement, \"Weekly reach of the organization's artifacts\": /review rises "+
				"in week %d, where a marker on every time chart says it was first used. Whether it helps "+
				"is the cost-per-edit story.", st.Review),
		},
		{
			Name: "connector-rarely-called",
			Planted: fmt.Sprintf("A Jira connector is set up for everyone in week %d. Its tools are in every "+
				"session's context from then on, and about %s people call it each week.",
				st.Connector, count(jiraCallersPerWeek)),
			Shows: fmt.Sprintf("Enablement, \"Weekly reach of the organization's artifacts\": jira's reach "+
				"stays at a few people. \"Context every session starts with\": a step up in week %d, "+
				"because every session carries its tools.", st.Connector),
		},
		{
			Name: "skill-rarely-triggers",
			Planted: fmt.Sprintf("The api-guidelines skill is set up for everyone in week %d, and the model "+
				"rarely uses it.", st.Connector),
			Shows: "Enablement, \"Weekly reach of the organization's artifacts\": a few people a week.",
		},
		{
			Name: "flaky-tests",
			Planted: fmt.Sprintf("The %s team's test command fails now and then. Sessions run it 3 to 7 "+
				"times in a row, the model reading each failure, until it passes.", st.FlakyTeam),
			Shows: "Enablement, \"Failing calls repeated in a row\": Bash's loops, every week, far above " +
				"every other tool's.",
			Gap: "Which repository the loops are in: Claude Code does not send the command (INV-5), and the " +
				"repository waits until real sessions have confirmed how it arrives (ADR-0035). By design " +
				"the chart never says which team.",
		},
		{
			Name: "compactions-by-style",
			Planted: "Compactions per session differ widely by way of working: dozens in a long autonomous " +
				"run, almost none in a session that asks one quick question.",
			Gap: "No panel shows compactions. The dashboard's review dropped the fleet's average, which hid " +
				"that a few long sessions make most of them, and no view shows how they spread across " +
				"sessions. gold_friction_daily still counts them.",
		},
		{
			Name: "permission-waits",
			Planted: "People in the default permission mode answer prompts for edits and commands; people in " +
				"auto mode answer none.",
			Gap: "No panel shows permission waits. The dashboard's review dropped the chart: in a fleet " +
				"that works in auto mode almost nobody waits, so the chart reports a success that did not " +
				"occur (ADR-0031). gold_friction_daily still counts them.",
		},
		{
			Name: "heavy-tail",
			Planted: "A few people spend much of the money: monthly spend runs from about $100 to $4,000, " +
				"and does not follow session counts.",
			Gap: "By design no view shows one person's spend to anyone else (the per-person rule, INV-3). " +
				"The view of cost over a published budget (ADR-0033) is not built.",
		},
		{
			Name: "expensive-default",
			Planted: fmt.Sprintf("The %s team makes the most expensive model its default in week %d, and "+
				"its spend for the same work rises about three times.", st.ExpensiveTeam, st.ExpensiveModel),
			Shows: fmt.Sprintf("Enablement, \"Weekly spend by team\": the %s team's spend rises from week %d. "+
				"\"Weekly spend by model\": %s appears the same week, which says why.",
				st.ExpensiveTeam, st.ExpensiveModel, expensiveModel),
		},
		{
			Name: "caching-bug",
			Planted: fmt.Sprintf("Claude Code releases in %s have a caching bug that raises cost per request. "+
				"A release early in week %d fixes it, and people upgrade at their own pace.", bug, st.Bug[1]+1),
			Shows: fmt.Sprintf("Enablement, \"Claude Code releases: how much of the context each reuses from "+
				"cache\": the releases from %s read less of their context from the cache than the rest.", bug),
		},
		{
			Name: "overnight-loops",
			Planted: night + " a long loop running overnight, a few nights a week, for hours with nobody " +
				"at the keyboard.",
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
			Planted: fmt.Sprintf("A team of %s (%s), a command %s people use, and personal commands, "+
				"subagents and connectors no more than two people use.", count(small.People), small.Name, count(db)),
			Shows: "Every view: the team is always \"other\", and so are the names, every day and every " +
				"week (the five-person minimum, ADR-0029).",
		},
		{
			Name: "usage-gaming",
			Planted: fmt.Sprintf("From week %d one person in the %s team runs /review from a script, dozens "+
				"of times a working day, in sessions that do nothing else.", st.Gamer, st.GamerTeam),
			Shows: fmt.Sprintf("Enablement, \"Weekly reach of the organization's artifacts\" and \"The "+
				"organization's artifacts in use\" count people, not sessions: /review's reach does not "+
				"jump in week %d, when the script's sessions start.", st.Gamer),
		},
		{
			Name: "two-changes-at-once",
			Planted: fmt.Sprintf("perf-budget, a skill of the organization's, is rolled out to the %s teams "+
				"in week %d, the week the caching bug arrives. It changes nothing, and those teams' cost "+
				"per request rises.", join(st.PerfBudgetTeams), st.PerfBudget),
			Gap: "Nothing warns that two things changed in the same week. A marker says perf-budget was " +
				"first used in the week spend rises, and only the releases table, read separately, shows the " +
				"caching bug, so a reader credits the skill with the bug.",
		},
	}
}

// deployCheckSpread is what a preset's /deploy-check steps add up to: the week it starts and in
// how many teams, the week its people first reach the five-person minimum, so the views name it,
// and how many teams it is in by the end.
type deployCheckSpread struct {
	first, firstTeams, named, lastTeams int
}

func (st Stories) spread(weeks int) deployCheckSpread {
	var s deployCheckSpread
	people := 0
	seen := map[string]bool{}
	for _, step := range st.DeployCheck {
		if step.Week > weeks {
			break
		}
		if s.first == 0 {
			s.first = step.Week
		}
		if step.Week == s.first && !seen[step.Team] {
			s.firstTeams++
		}
		seen[step.Team] = true
		people += step.People
		if s.named == 0 && people >= MinGroupSize {
			s.named = step.Week
		}
	}
	s.lastTeams = len(seen)
	return s
}

// smallTeam is the preset's smallest team: the one the five-person minimum always hides.
func smallTeam(p Preset) TeamSpec {
	small := p.Teams[0]
	for _, t := range p.Teams {
		if t.People < small.People {
			small = t
		}
	}
	return small
}

// count writes a small number as a word, as the stories do.
func count(n int) string {
	words := []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

func upper(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// teams is "one team", "two teams" and so on.
func teams(n int) string {
	if n == 1 {
		return "one team"
	}
	return count(n) + " teams"
}

// onV2 names the teams that stay on v2: "mobile team", "mobile and web teams".
func onV2(teams []string) string {
	if len(teams) == 1 {
		return teams[0] + " team"
	}
	return join(teams) + " teams"
}

// join lists names as a sentence does: "a", "a and b", "a, b and c".
func join(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
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

// Catalog is a preset's list of stories, in order: the gap list is the ones with a Gap.
func Catalog(p Preset) []Story { return catalog(p, p.Weeks) }
