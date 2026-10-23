package madeup_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/amanachenko/cardo/internal/madeup"
)

// The stories as the review preset plants them, read back from its answer key. Each test is what
// a story's text promises: if a change to the generator or its placeholders breaks the promise,
// the text or the change has to give.

var (
	reviewOnce sync.Once
	reviewKey  *madeup.Key
)

// review is the review preset's twelve weeks, invented once and sent nowhere.
func review(t *testing.T) *madeup.Key {
	t.Helper()
	reviewOnce.Do(func() { reviewKey = generate(t, madeup.Presets["review"], 12, 8, discard{}) })
	if reviewKey == nil {
		t.Fatal("the review preset could not be generated")
	}
	return reviewKey
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

// Every story is on the list, and each says either where it shows or what no view shows. The
// list of gaps is the stories with a gap: none may be left off it by having neither.
func TestEveryStoryShowsOrIsAGap(t *testing.T) {
	k := review(t)
	if len(k.Stories) != len(madeup.Catalog()) {
		t.Errorf("the review preset's key has %d stories; the catalog has %d. Every story happens in its twelve weeks.",
			len(k.Stories), len(madeup.Catalog()))
	}
	for _, s := range madeup.Catalog() {
		if s.Shows == "" && s.Gap == "" {
			t.Errorf("the %s story says neither where it shows nor what no view shows", s.Name)
		}
		if s.Planted == "" {
			t.Errorf("the %s story does not say what was planted", s.Name)
		}
	}
}

// The small organization squeezes every story into two weeks, so the requests the other tests
// decode include each of them.
func TestSmallOrganizationHasEveryStory(t *testing.T) {
	k, _ := sent(t)
	if len(k.Stories) != len(madeup.Catalog()) {
		var names []string
		for _, s := range k.Stories {
			names = append(names, s.Name)
		}
		t.Fatalf("the small organization's key has %d of the %d stories: %s",
			len(k.Stories), len(madeup.Catalog()), strings.Join(names, ", "))
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

func TestDeployCheckCrossesFiveInWeekFive(t *testing.T) {
	k := review(t)
	for w := 2; w <= 4; w++ {
		if n := figure(t, k, "deploy-check-spreads", fmt.Sprintf("week %d: people using it", w)); n < 1 || n >= madeup.MinGroupSize {
			t.Errorf("week %d: /deploy-check has %v people; it should have some, and fewer than five, so the view shows \"other\"", w, n)
		}
	}
	if n := figure(t, k, "deploy-check-spreads", "week 5: people using it"); n < madeup.MinGroupSize {
		t.Errorf("week 5: /deploy-check has %v people; the story has it named from week 5", n)
	}
	if n := figure(t, k, "deploy-check-spreads", "week 5: teams using it"); n != 1 {
		t.Errorf("week 5: /deploy-check is in %v teams; it starts in one", n)
	}
	if n := figure(t, k, "deploy-check-spreads", "week 12: teams using it"); n != 4 {
		t.Errorf("week 12: /deploy-check is in %v teams; the story has it reach four", n)
	}
}

func TestInstructionsV3AddsAFifth(t *testing.T) {
	k := review(t)
	if n := figure(t, k, "instructions-v3", "week 3: people loading v3"); n != 0 {
		t.Errorf("%v people load v3 in week 3, before it ships", n)
	}
	for _, w := range []int{4, 12} {
		if n := figure(t, k, "instructions-v3", fmt.Sprintf("week %d: people loading v2", w)); n == 0 {
			t.Errorf("week %d: nobody loads v2; two teams stay on it", w)
		}
		if n := figure(t, k, "instructions-v3", fmt.Sprintf("week %d: people loading v3", w)); n == 0 {
			t.Errorf("week %d: nobody loads v3", w)
		}
	}
	if r := figure(t, k, "instructions-v3", "v3 against v2"); r < 1.1 || r > 1.3 {
		t.Errorf("sessions loading v3 start from %.2f times the context of those loading v2; the story says about a fifth more", r)
	}
}

func TestReviewRolloutAndTheRarelyUsed(t *testing.T) {
	k := review(t)
	if n := figure(t, k, "review-rollout", "week 5: people using it"); n != 0 {
		t.Errorf("%v people use /review in week 5, before it is rolled out", n)
	}
	if n := figure(t, k, "review-rollout", "week 6: people using it"); n < 25 {
		t.Errorf("only %v people use /review in the week it is rolled out to all but two small teams", n)
	}
	for w := 6; w <= 12; w++ {
		if n := figure(t, k, "connector-rarely-called", fmt.Sprintf("week %d: people calling it", w)); n > 5 {
			t.Errorf("week %d: %v people call the Jira connector; about three should", w, n)
		}
		if n := figure(t, k, "skill-rarely-triggers", fmt.Sprintf("week %d: people whose sessions used it", w)); n > 8 {
			t.Errorf("week %d: api-guidelines was used in %v people's sessions; it should be rare", w, n)
		}
	}
}

func TestCostStories(t *testing.T) {
	k := review(t)
	if r := figure(t, k, "expensive-default", "weeks 8 and 9 against weeks 6 and 7"); r < 2.5 || r > 3.5 {
		t.Errorf("platform spends %.2f times as much after moving to the most expensive model; the story says about three times", r)
	}

	before := figure(t, k, "caching-bug", "week 9: cost per request ($)")
	during := figure(t, k, "caching-bug", "week 11: cost per request ($)")
	if during < 1.3*before {
		t.Errorf("cost per request is $%.4f in week 11, when nearly everyone runs a release with the bug, against $%.4f in week 9", during, before)
	}
	if figure(t, k, "caching-bug", "week 9: share of sessions on a release with the bug") != 0 {
		t.Error("sessions in week 9 run a release with the bug, before it was released")
	}
	if figure(t, k, "caching-bug", "week 12: share of sessions on a release with the bug") >=
		figure(t, k, "caching-bug", "week 11: share of sessions on a release with the bug") {
		t.Error("as many sessions run a release with the bug in week 12, after the fix, as in week 11")
	}

	for _, g := range []string{"payments and data", "the other teams"} {
		if r := figure(t, k, "two-changes-at-once", "week 10 against week 9, "+g); r < 1.15 {
			t.Errorf("cost per request for %s rises only %.2f times in week 10; the bug should raise it everywhere", g, r)
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
}

func TestWorkingStories(t *testing.T) {
	k := review(t)
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
	script := figure(t, k, "usage-gaming", "week 7: of them, from the script")
	if all := figure(t, k, "usage-gaming", "week 7: /review sessions"); script < all/2 {
		t.Errorf("the gaming script ran %v of week 7's %v /review sessions; it should be most of them", script, all)
	}
	if figure(t, k, "usage-gaming", "week 6: of them, from the script") != 0 {
		t.Error("the gaming script ran before week 7")
	}
}
