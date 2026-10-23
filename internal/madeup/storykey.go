package madeup

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// key joins the parts of a story sum's name.
func key(parts ...any) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprint(&b, p)
	}
	return b.String()
}

// storyFigures is the list of stories that happen in the history, each with what the generator
// measured in what it sent.
func storyFigures(o *Org, k *Key, sums map[string]float64) []KeyStory {
	st := o.Preset.Stories
	weeks := o.Days / 7
	in := func(w int) bool { return w > 0 && w <= weeks }
	weekOf := func(w int) string { return o.Start.AddDate(0, 0, 7*(w-1)).Format(time.DateOnly) }
	artifact := func(w int, kind, name string) (people, sessions float64) {
		for _, kw := range k.Weeks {
			if kw.Week != weekOf(w) {
				continue
			}
			for _, a := range kw.Artifacts {
				if a.Kind == kind && a.Name == name {
					people += float64(a.People)
					sessions += float64(a.Sessions)
				}
			}
		}
		return people, sessions
	}
	ratio := func(a, b float64) float64 {
		if b == 0 {
			return 0
		}
		return a / b
	}
	perEdit := func(prefix string) float64 { return ratio(sums[prefix+"|cost"], sums[prefix+"|edits"]) }
	perRequest := func(prefix string) float64 { return ratio(sums[prefix+"|cost"], sums[prefix+"|requests"]) }
	teamWeek := func(team string, w int) (cost float64) {
		for _, d := range k.Days {
			if d.Team == team && d.Day >= weekOf(w) && d.Day < weekOf(w+1) {
				cost += d.CostUSD
			}
		}
		return cost
	}
	deployCheck := len(st.DeployCheck) > 0 && in(st.DeployCheck[0].Week)

	var out []KeyStory
	for _, c := range catalog {
		ks := KeyStory{Name: c.Name, Planted: c.Planted, Shows: c.Shows, Gap: c.Gap}
		fig := func(name string, v float64) {
			ks.Figures = append(ks.Figures, KeyFigure{name, math.Round(v*1e4) / 1e4})
		}
		happens := true
		switch c.Name {
		case "deploy-check-spreads":
			if happens = deployCheck; !happens {
				break
			}
			for w := st.DeployCheck[0].Week; w <= weeks; w++ {
				people, _ := artifact(w, "skill", "deploy-check")
				teams := 0
				for _, t := range o.Preset.Teams {
					if sums[key("deploy-check|team", w, t.Name)] > 0 {
						teams++
					}
				}
				fig(fmt.Sprintf("week %d: people using it", w), people)
				fig(fmt.Sprintf("week %d: teams using it", w), float64(teams))
			}
		case "instructions-v3":
			if happens = in(st.InstructionsV3); !happens {
				break
			}
			for _, w := range []int{st.InstructionsV3 - 1, st.InstructionsV3, weeks} {
				if w < 1 {
					continue
				}
				v2, _ := artifact(w, "instructions", "example-standards-v2.md")
				v3, _ := artifact(w, "instructions", "example-standards-v3.md")
				fig(fmt.Sprintf("week %d: people loading v2", w), v2)
				fig(fmt.Sprintf("week %d: people loading v3", w), v3)
			}
			v2 := ratio(sums["standards|v2|start-context"], sums["standards|v2|sessions"])
			v3 := ratio(sums["standards|v3|start-context"], sums["standards|v3|sessions"])
			fig(fmt.Sprintf("mean context a session loading v2 starts from, from week %d (tokens)", st.InstructionsV3), v2)
			fig(fmt.Sprintf("mean context a session loading v3 starts from, from week %d (tokens)", st.InstructionsV3), v3)
			fig("v3 against v2", ratio(v3, v2))
		case "review-rollout":
			if happens = in(st.Review); !happens {
				break
			}
			for w := max(1, st.Review-1); w <= weeks; w++ {
				people, _ := artifact(w, "skill", "review")
				fig(fmt.Sprintf("week %d: people using it", w), people)
			}
		case "connector-rarely-called":
			if happens = in(st.Connector); !happens {
				break
			}
			for w := st.Connector; w <= weeks; w++ {
				people, _ := artifact(w, "mcp", "jira")
				fig(fmt.Sprintf("week %d: people calling it", w), people)
			}
			fig("tokens its tools add to every session's context", connectorTokens)
		case "skill-rarely-triggers":
			if happens = in(st.Connector); !happens {
				break
			}
			for w := st.Connector; w <= weeks; w++ {
				people, _ := artifact(w, "skill", "api-guidelines")
				fig(fmt.Sprintf("week %d: people whose sessions used it", w), people)
			}
		case "flaky-tests":
			if happens = st.FlakyTeam != ""; !happens {
				break
			}
			fig("loops", sums["flaky|loops"])
			fig("failed runs", sums["flaky|failed"])
			fig("requests the reruns added", sums["flaky|requests"])
			fig("what those requests cost ($)", sums["flaky|cost"])
		case "compactions-by-style":
			for _, s := range Styles {
				if n := sums["style|"+s.Name+"|sessions"]; n > 0 {
					fig("compactions per session, "+s.Name, ratio(sums["style|"+s.Name+"|compactions"], n))
				}
			}
		case "permission-waits":
			for _, m := range []string{"default", "acceptEdits", "auto"} {
				n := sums["mode|"+m+"|prompts"]
				fig("prompts answered, "+m+" mode", n)
				fig("mean wait, "+m+" mode (s)", ratio(sums["mode|"+m+"|wait-ms"], n)/1000)
			}
		case "heavy-tail":
			months := float64(o.Days) / 30.4
			var spend []float64
			total := 0.0
			for _, p := range k.People {
				spend = append(spend, p.CostUSD/months)
				total += p.CostUSD / months
			}
			slices.Sort(spend)
			slices.Reverse(spend)
			top := 0.0
			for _, v := range spend[:max(1, len(spend)/10)] {
				top += v
			}
			median := spend[len(spend)/2]
			fig("share of spend from the top tenth of people", ratio(top, total))
			fig("median monthly spend ($)", median)
			fig("mean monthly spend ($)", ratio(total, float64(len(spend))))
			fig("highest monthly spend against the median", ratio(spend[0], median))
		case "expensive-default":
			if happens = in(st.ExpensiveModel); !happens {
				break
			}
			w := st.ExpensiveModel
			for x := max(1, w-2); x <= min(weeks, w+1); x++ {
				fig(fmt.Sprintf("week %d: %s's spend ($)", x, st.ExpensiveTeam), teamWeek(st.ExpensiveTeam, x))
			}
			if w >= 3 && w+1 <= weeks {
				fig(fmt.Sprintf("weeks %d and %d against weeks %d and %d", w, w+1, w-2, w-1),
					ratio(teamWeek(st.ExpensiveTeam, w)+teamWeek(st.ExpensiveTeam, w+1),
						teamWeek(st.ExpensiveTeam, w-2)+teamWeek(st.ExpensiveTeam, w-1)))
			}
		case "caching-bug":
			if happens = in(st.Bug[0]); !happens {
				break
			}
			for w := max(1, st.Bug[0]-2); w <= min(weeks, st.Bug[1]+1); w++ {
				fig(fmt.Sprintf("week %d: cost per request ($)", w), perRequest(key("week", w)))
				fig(fmt.Sprintf("week %d: share of sessions on a release with the bug", w),
					ratio(sums[key("week", w, "buggy")], sums[key("week", w)+"|sessions"]))
			}
			fig("what the bug added to spend ($)", sums["bug|added"])
			spent := 0.0
			for w := st.Bug[0]; w <= min(weeks, st.Bug[1]+1); w++ {
				spent += sums[key("week", w)+"|cost"]
			}
			fig(fmt.Sprintf("share of spend in weeks %d to %d", st.Bug[0], min(weeks, st.Bug[1]+1)),
				ratio(sums["bug|added"], spent))
		case "overnight-loops":
			if happens = st.Overnight > 0; !happens {
				break
			}
			fig("people", float64(st.Overnight))
			fig("sessions", sums["overnight|sessions"])
			fig("mean hours each", ratio(sums["overnight|hours"], sums["overnight|sessions"]))
			fig("what they cost ($)", sums["overnight|cost"])
			fig("share of all spend", ratio(sums["overnight|cost"], k.Totals.CostUSD))
		case "cost-per-edit":
			if happens = deployCheck || in(st.Review); !happens {
				break
			}
			if deployCheck {
				with, without := perEdit("deploy-check|with"), perEdit("deploy-check|without")
				fig("cost per accepted edit, sessions using /deploy-check ($)", with)
				fig("cost per accepted edit, the same people's other sessions ($)", without)
				fig("/deploy-check against the rest", ratio(with, without))
			}
			if in(st.Review) {
				with, without := perEdit("review|with"), perEdit("review|without")
				fig("cost per accepted edit, sessions using /review ($)", with)
				fig("cost per accepted edit, other sessions in the same teams ($)", without)
				fig("/review against the rest", ratio(with, without))
			}
		case "privacy-floor":
			for _, t := range k.Teams {
				if t.People < MinGroupSize {
					fig("people in "+t.Name, float64(t.People))
				}
			}
			most := map[[2]string]int{}
			for _, w := range k.Weeks {
				for _, a := range w.Artifacts {
					if !a.Org && !a.Builtin && a.Name != "" {
						most[[2]string{a.Kind, a.Name}] = max(most[[2]string{a.Kind, a.Name}], a.People)
					}
				}
			}
			never, highest := 0, 0
			for _, n := range most {
				if n < MinGroupSize {
					never++
					highest = max(highest, n)
				}
			}
			fig("home-grown names never shown", float64(never))
			fig("most people one of them had in a week", float64(highest))
		case "usage-gaming":
			if happens = in(st.Gamer); !happens {
				break
			}
			for w := max(1, st.Gamer-1); w <= weeks; w++ {
				people, sessions := artifact(w, "skill", "review")
				fig(fmt.Sprintf("week %d: /review sessions", w), sessions)
				fig(fmt.Sprintf("week %d: of them, from the script", w), sums[key("gaming", w, "sessions")])
				fig(fmt.Sprintf("week %d: /review people", w), people)
			}
		case "two-changes-at-once":
			if happens = in(st.PerfBudget) && st.PerfBudget > 1; !happens {
				break
			}
			w := st.PerfBudget
			for _, g := range []string{"teams", "rest"} {
				name := strings.Join(st.PerfBudgetTeams, " and ")
				if g == "rest" {
					name = "the other teams"
				}
				before, after := perRequest(key("overlap", w-1, g)), perRequest(key("overlap", w, g))
				fig(fmt.Sprintf("week %d: cost per request, %s ($)", w-1, name), before)
				fig(fmt.Sprintf("week %d: cost per request, %s ($)", w, name), after)
				fig(fmt.Sprintf("week %d against week %d, %s", w, w-1, name), ratio(after, before))
			}
			fig("what perf-budget changes about a session", 0)
		}
		if happens {
			out = append(out, ks)
		}
	}
	return out
}
