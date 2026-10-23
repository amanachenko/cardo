package madeup_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/amanachenko/cardo/internal/madeup"
)

// The stories as each preset plants them, read back from its answer key. Each test is what a
// story's text promises: if a change to the generator, a preset or the placeholders breaks the
// promise, the text or the change has to give.

// The presets whose stories are held to their text: the one people look at, and the one a CI job
// checks. The large preset is for size, and is held only to its shape (TestPresetsCanTellTheirStories).
var storyPresets = []string{"review", "ci"}

var (
	keysMu sync.Mutex
	keys   = map[string]*madeup.Key{}
)

// keyOf is a preset's whole history, invented once and sent nowhere.
func keyOf(t *testing.T, name string) *madeup.Key {
	t.Helper()
	keysMu.Lock()
	defer keysMu.Unlock()
	if keys[name] == nil {
		p := madeup.Presets[name]
		keys[name] = generate(t, p, p.Weeks, 8, discard{})
	}
	return keys[name]
}

// eachPreset runs a story's test on each preset in storyPresets.
func eachPreset(t *testing.T, test func(t *testing.T, p madeup.Preset, k *madeup.Key)) {
	for _, name := range storyPresets {
		t.Run(name, func(t *testing.T) { test(t, madeup.Presets[name], keyOf(t, name)) })
	}
}

// figure is one story's figure, by name.
func figure(t *testing.T, k *madeup.Key, story, name string) float64 {
	t.Helper()
	for _, s := range k.Stories {
		if s.Name != story {
			continue
		}
		for _, f := range s.Figures {
			if f.Name == name {
				return f.Value
			}
		}
		t.Fatalf("the %s story has no figure %q", story, name)
	}
	t.Fatalf("the key has no %s story", story)
	return 0
}

// and lists team names as the figures do: "a", "a and b", "a, b and c".
func and(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// Every story is on the list, and each says either where it shows or what no view shows. The
// list of gaps is the stories with a gap: none may be left off it by having neither.
func TestEveryStoryShowsOrIsAGap(t *testing.T) {
	eachPreset(t, func(t *testing.T, p madeup.Preset, k *madeup.Key) {
		if len(k.Stories) != len(madeup.Catalog(p)) {
			t.Errorf("the key has %d stories; the catalog has %d. Every story happens in the preset's %d weeks.",
				len(k.Stories), len(madeup.Catalog(p)), p.Weeks)
		}
		for _, s := range madeup.Catalog(p) {
			if s.Shows == "" && s.Gap == "" {
				t.Errorf("the %s story says neither where it shows nor what no view shows", s.Name)
			}
			if s.Planted == "" {
				t.Errorf("the %s story does not say what was planted", s.Name)
			}
		}
	})
}

// Every preset can tell every story in its weeks, in the order the stories need. This is what the
// story tests above would find, checked without inventing anything, so it covers the large preset
// too.
func TestPresetsCanTellTheirStories(t *testing.T) {
	for name, p := range madeup.Presets {
		t.Run(name, func(t *testing.T) {
			st := p.Stories
			sizes := map[string]int{}
			for _, tm := range p.Teams {
				sizes[tm.Name] = tm.People
			}
			named := []string{st.ExpensiveTeam, st.FlakyTeam, st.GamerTeam}
			named = append(named, st.StayOnV2...)
			named = append(named, st.NoReview...)
			named = append(named, st.PerfBudgetTeams...)
			for _, s := range st.DeployCheck {
				named = append(named, s.Team)
			}
			for _, tm := range named {
				if sizes[tm] == 0 {
					t.Errorf("a story names the team %q, which the preset does not have", tm)
				}
			}
			small := false
			for _, tm := range p.Teams {
				small = small || tm.People < madeup.MinGroupSize
			}
			if !small {
				t.Error("no team is under the five-person minimum, so the privacy-floor story has no team to hide")
			}

			in := func(story string, w int) {
				if w < 1 || w > p.Weeks {
					t.Errorf("%s happens in week %d, outside the preset's %d weeks", story, w, p.Weeks)
				}
			}
			for _, c := range []struct {
				story string
				week  int
			}{
				{"instructions-v3", st.InstructionsV3}, {"review-rollout", st.Review},
				{"connector-rarely-called", st.Connector}, {"expensive-default", st.ExpensiveModel},
				{"caching-bug", st.Bug[0]}, {"two-changes-at-once", st.PerfBudget}, {"usage-gaming", st.Gamer},
			} {
				in(c.story, c.week)
			}
			// Each rollout has a week before it, to show nobody using it yet.
			for _, w := range []int{st.InstructionsV3, st.Review, st.Gamer} {
				if w < 2 {
					t.Errorf("a story starts in week %d, with no week before it to compare with", w)
				}
			}
			// The expensive model is measured over two weeks before and two after, all of them with
			// the rollouts in and the bug not yet: otherwise its ratio is theirs too.
			e := st.ExpensiveModel
			if e-2 < max(st.InstructionsV3, st.Review, st.Connector) || st.Bug[0] <= e+1 {
				t.Errorf("the expensive model in week %d is not two clean weeks after the rollouts (weeks %d, %d "+
					"and %d) and two before the caching bug (week %d)", e, st.InstructionsV3, st.Review, st.Connector, st.Bug[0])
			}
			// The bug is fixed inside the history, so the history shows it ending.
			if st.Bug[1] < st.Bug[0] || st.Bug[1]+1 > p.Weeks {
				t.Errorf("the caching bug runs from week %d to week %d, and its fix has to come in the history's %d weeks",
					st.Bug[0], st.Bug[1], p.Weeks)
			}
			// Two changes at once is the skill arriving with the bug.
			if st.PerfBudget != st.Bug[0] {
				t.Errorf("perf-budget arrives in week %d and the caching bug in week %d; the story is that they come together",
					st.PerfBudget, st.Bug[0])
			}

			people, first := 0, st.DeployCheck[0].Week
			teams := map[string]bool{}
			starting := map[string]bool{}
			for i, s := range st.DeployCheck {
				if i > 0 && s.Week < st.DeployCheck[i-1].Week {
					t.Errorf("/deploy-check's steps go back from week %d to week %d", st.DeployCheck[i-1].Week, s.Week)
				}
				if s.Week == first {
					starting[s.Team] = true
				}
				teams[s.Team] = true
				people += s.People
			}
			if len(starting) != 1 {
				t.Errorf("/deploy-check starts in %d teams; it is one engineer's command, and starts in one", len(starting))
			}
			if people < madeup.MinGroupSize+2 {
				t.Errorf("/deploy-check reaches %d people; it has to cross the five-person minimum with room for a holiday", people)
			}
			if len(teams) < 3 {
				t.Errorf("/deploy-check reaches %d teams; spreading across teams is its story", len(teams))
			}
		})
	}
}

// The small organization squeezes every story into two weeks, so the requests the other tests
// decode include each of them.
func TestSmallOrganizationHasEveryStory(t *testing.T) {
	k, _ := sent(t)
	if len(k.Stories) != len(madeup.Catalog(small)) {
		var names []string
		for _, s := range k.Stories {
			names = append(names, s.Name)
		}
		t.Fatalf("the small organization's key has %d of the %d stories: %s",
			len(k.Stories), len(madeup.Catalog(small)), strings.Join(names, ", "))
	}
	for _, c := range []struct{ story, figure string }{
		{"deploy-check-spreads", "week 1: people using it"},
		{"connector-rarely-called", "week 2: people calling it"},
		{"flaky-tests", "loops"},
		{"overnight-loops", "sessions"},
		{"usage-gaming", "week 2: of them, from the script"},
		{"caching-bug", "week 1: share of sessions on a release with the bug"},
		{"instructions-v3", "week 2: people loading v3"},
		{"review-rollout", "week 2: people using it"},
	} {
		if figure(t, k, c.story, c.figure) <= 0 {
			t.Errorf("the small organization's %s story has %s = 0; nothing of it was sent", c.story, c.figure)
		}
	}
}

// /deploy-check is "other" until the week its people reach five, and named every week after.
func TestDeployCheckCrossesFive(t *testing.T) {
	eachPreset(t, func(t *testing.T, p madeup.Preset, k *madeup.Key) {
		steps := p.Stories.DeployCheck
		first, named, people := steps[0].Week, 0, 0
		teams := map[string]bool{}
		for _, s := range steps {
			people += s.People
			teams[s.Team] = true
			if named == 0 && people >= madeup.MinGroupSize {
				named = s.Week
			}
		}
		for w := first; w < named; w++ {
			if n := figure(t, k, "deploy-check-spreads", fmt.Sprintf("week %d: people using it", w)); n < 1 || n >= madeup.MinGroupSize {
				t.Errorf("week %d: /deploy-check has %v people; it should have some, and fewer than five, so the view shows \"other\"", w, n)
			}
		}
		for w := named; w <= p.Weeks; w++ {
			if n := figure(t, k, "deploy-check-spreads", fmt.Sprintf("week %d: people using it", w)); n < madeup.MinGroupSize {
				t.Errorf("week %d: /deploy-check has %v people; the story has it named from week %d on", w, n, named)
			}
		}
		if n := figure(t, k, "deploy-check-spreads", fmt.Sprintf("week %d: teams using it", first)); n != 1 {
			t.Errorf("week %d: /deploy-check is in %v teams; it starts in one", first, n)
		}
		if n := figure(t, k, "deploy-check-spreads", fmt.Sprintf("week %d: teams using it", p.Weeks)); n != float64(len(teams)) {
			t.Errorf("week %d: /deploy-check is in %v teams; the story has it reach %d", p.Weeks, n, len(teams))
		}
	})
}

func TestInstructionsV3AddsAFifth(t *testing.T) {
	eachPreset(t, func(t *testing.T, p madeup.Preset, k *madeup.Key) {
		v3 := p.Stories.InstructionsV3
		if n := figure(t, k, "instructions-v3", fmt.Sprintf("week %d: people loading v3", v3-1)); n != 0 {
			t.Errorf("%v people load v3 in week %d, before it ships", n, v3-1)
		}
		for _, w := range []int{v3, p.Weeks} {
			if n := figure(t, k, "instructions-v3", fmt.Sprintf("week %d: people loading v2", w)); n == 0 {
				t.Errorf("week %d: nobody loads v2; %s stays on it", w, and(p.Stories.StayOnV2))
			}
			if n := figure(t, k, "instructions-v3", fmt.Sprintf("week %d: people loading v3", w)); n == 0 {
				t.Errorf("week %d: nobody loads v3", w)
			}
		}
		if r := figure(t, k, "instructions-v3", "v3 against v2"); r < 1.1 || r > 1.3 {
			t.Errorf("sessions loading v3 start from %.2f times the context of those loading v2; the story says about a fifth more", r)
		}
	})
}

func TestReviewRolloutAndTheRarelyUsed(t *testing.T) {
	eachPreset(t, func(t *testing.T, p madeup.Preset, k *madeup.Key) {
		st := p.Stories
		if n := figure(t, k, "review-rollout", fmt.Sprintf("week %d: people using it", st.Review-1)); n != 0 {
			t.Errorf("%v people use /review in week %d, before it is rolled out", n, st.Review-1)
		}
		reached := 0
		for _, tm := range p.Teams {
			if !slices.Contains(st.NoReview, tm.Name) {
				reached += tm.People
			}
		}
		if n := figure(t, k, "review-rollout", fmt.Sprintf("week %d: people using it", st.Review)); n < float64(reached)/2 {
			t.Errorf("only %v of the %d people it was rolled out to use /review in its first week", n, reached)
		}
		for w := st.Connector; w <= p.Weeks; w++ {
			if n := figure(t, k, "connector-rarely-called", fmt.Sprintf("week %d: people calling it", w)); n > 5 {
				t.Errorf("week %d: %v people call the Jira connector; about three should", w, n)
			}
			if n := figure(t, k, "skill-rarely-triggers", fmt.Sprintf("week %d: people whose sessions used it", w)); n > 8 {
				t.Errorf("week %d: api-guidelines was used in %v people's sessions; it should be rare", w, n)
			}
		}
	})
}

func TestCostStories(t *testing.T) {
	eachPreset(t, func(t *testing.T, p madeup.Preset, k *madeup.Key) {
		st := p.Stories
		e := st.ExpensiveModel
		if r := figure(t, k, "expensive-default", fmt.Sprintf("weeks %d and %d against weeks %d and %d", e, e+1, e-2, e-1)); r < 2.5 || r > 3.5 {
			t.Errorf("%s spends %.2f times as much after moving to the most expensive model; the story says about three times", st.ExpensiveTeam, r)
		}

		before, last := st.Bug[0]-1, st.Bug[1]
		if b, d := figure(t, k, "caching-bug", fmt.Sprintf("week %d: cost per request ($)", before)),
			figure(t, k, "caching-bug", fmt.Sprintf("week %d: cost per request ($)", last)); d < 1.3*b {
			t.Errorf("cost per request is $%.4f in week %d, when most sessions run a release with the bug, against $%.4f in week %d", d, last, b, before)
		}
		share := func(w int) float64 {
			return figure(t, k, "caching-bug", fmt.Sprintf("week %d: share of sessions on a release with the bug", w))
		}
		if share(before) != 0 {
			t.Errorf("sessions in week %d run a release with the bug, before it was released", before)
		}
		if share(last+1) >= share(last) {
			t.Errorf("as many sessions run a release with the bug in week %d, after the fix, as in week %d", last+1, last)
		}

		w := st.PerfBudget
		for _, g := range []string{and(st.PerfBudgetTeams), "the other teams"} {
			if r := figure(t, k, "two-changes-at-once", fmt.Sprintf("week %d against week %d, %s", w, w-1, g)); r < 1.15 {
				t.Errorf("cost per request for %s rises only %.2f times in week %d; the bug should raise it everywhere", g, r, w)
			}
		}

		if r := figure(t, k, "cost-per-edit", "/deploy-check against the rest"); r < 0.6 || r > 0.8 {
			t.Errorf("sessions using /deploy-check cost %.2f times as much per accepted edit; the story says about 30%% less", r)
		}
		if r := figure(t, k, "cost-per-edit", "/review against the rest"); r < 0.9 || r > 1.1 {
			t.Errorf("sessions using /review cost %.2f times as much per accepted edit; the story says no different", r)
		}
		if s := figure(t, k, "heavy-tail", "share of spend from the top tenth of people"); s < 0.3 {
			t.Errorf("the top tenth of people spend %.0f%% of the money; the tail should be heavier", 100*s)
		}
	})
}

func TestWorkingStories(t *testing.T) {
	eachPreset(t, func(t *testing.T, p madeup.Preset, k *madeup.Key) {
		loops := figure(t, k, "flaky-tests", "loops")
		if loops == 0 || figure(t, k, "flaky-tests", "failed runs") < 2*loops {
			t.Errorf("%v retry loops with %v failed runs; each loop fails at least twice",
				loops, figure(t, k, "flaky-tests", "failed runs"))
		}
		if long, steady := figure(t, k, "compactions-by-style", "compactions per session, long-runs"),
			figure(t, k, "compactions-by-style", "compactions per session, steady"); long < 5*steady {
			t.Errorf("long runs compact %.1f times a session and steady users %.1f; the story has them differ widely", long, steady)
		}
		if n := figure(t, k, "permission-waits", "prompts answered, auto mode"); n != 0 {
			t.Errorf("people in auto mode answered %v permission prompts", n)
		}
		if n := figure(t, k, "permission-waits", "prompts answered, default mode"); n == 0 {
			t.Error("nobody in the default mode answered a permission prompt")
		}
		if n, h := figure(t, k, "overnight-loops", "sessions"), figure(t, k, "overnight-loops", "mean hours each"); n < 20 || h < 3 {
			t.Errorf("%v overnight sessions of %.1f hours each; two people should leave hours-long loops running most weeks", n, h)
		}
		if n := figure(t, k, "privacy-floor", "most people one of them had in a week"); n >= madeup.MinGroupSize {
			t.Errorf("a name that is never shown had %v people in a week", n)
		}
		g := p.Stories.Gamer
		script := figure(t, k, "usage-gaming", fmt.Sprintf("week %d: of them, from the script", g))
		if all := figure(t, k, "usage-gaming", fmt.Sprintf("week %d: /review sessions", g)); script < all/2 {
			t.Errorf("the gaming script ran %v of week %d's %v /review sessions; it should be most of them", script, g, all)
		}
		if figure(t, k, "usage-gaming", fmt.Sprintf("week %d: of them, from the script", g-1)) != 0 {
			t.Errorf("the gaming script ran before week %d", g)
		}
	})
}
