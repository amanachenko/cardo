package madeup

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// A session, written event by event in the order Claude Code would send them.
//
// Each request sends the conversation so far. The prompt cache holds what the previous request
// sent, for an hour, so a request reads that from cache and writes the rest: the previous reply
// and whatever was added since. After an hour's pause, or a model switch, the cache is cold and
// the request writes far more. Past autoCompactTokens the conversation is compacted down to a
// summary.
//
// How many requests a session makes comes from its budget: the person's placeholder month
// divided into sessions, with noise. Its prompts and its context size come from the person's
// style. So two people with the same spend can have very different sessions.

type session struct {
	o *Org
	p *Person
	r *rand.Rand

	id       string
	kind     sessionKind
	week     int // the week of the history it starts in, from 1
	ws       *weekState
	version  string
	buggy    bool // the release has the caching bug
	scripted bool // started by a script: one prompt, every tool allowed, nobody at the keyboard
	terminal string
	repo     *Repo
	paths    paths
	resource []kv

	logs   []logRecord
	hooks  []hookEvent
	points []metric
	seq    int
	last   time.Time // the latest event so far

	t         time.Time // the session's own clock
	model     string
	planModel string // the person's own model, which the session's work is planned at
	prefix    int    // the conversation's fixed prefix: system prompt, tools, instructions files
	extra     int    // the part of the prefix the stories added, which the plan leaves out
	main      conv
	compactAt int
	promptID  string
	skill     string // the custom command or skill this prompt runs, which every request in it names
	first     string // the command the session's first prompt runs, if any
	editBoost bool   // Edit and Write are likelier: the session used /deploy-check
	callJira  bool   // the session calls the Jira connector once
	inRetry   bool   // the requests being sent are the flaky test command's reruns

	allowed              map[string]bool // tools a "don't ask again" answer allowed for the rest of the session
	usedInPrompt         map[string]bool
	nestedDone, ruleDone bool
	costByModel          map[string]float64
	tokensByModel        map[string]*[4]int

	tally sessionTally
}

// conv is one conversation's state: the main thread's, or a subagent's.
type conv struct {
	sent, out, cached int
	last              time.Time
}

type hookEvent struct {
	at   time.Time
	body []byte
}

// sessionTally is what one session adds to the answer key.
type sessionTally struct {
	id                     string
	start                  time.Time // its first OTel record
	prompts, requests      int
	cost                   float64
	hooks, records, points int
	uses                   []use

	// What the friction and context views count.
	mainRequests                  int
	startContext                  int // the context of its first main-thread request
	editProposals, editRejections int
	acceptedEdits                 int
	permissionPrompts             int
	permissionWaitMs              int64
	compactions, autoCompactions  int

	// What the stories' figures need.
	kind                   sessionKind
	week                   int
	first                  string
	buggy                  bool
	standards              int // the version of example-standards it loaded, if any
	retryLoops, failedRuns int
	retryRequests          int
	retryCost              float64
	bugCost                float64 // what the caching bug added
	end                    time.Time
}

type use struct {
	at                   time.Time
	kind, name, fileKind string
	org, builtin         bool
}

type paths struct {
	sep, home, cwd, transcript, scratchpad, managed string
}

func ms(n float64) time.Duration { return time.Duration(n * float64(time.Millisecond)) }

// sessionKind is what started a session.
type sessionKind int

const (
	worked    sessionKind = iota // the person, at their keyboard
	overnight                    // a long loop left running overnight
	gaming                       // the script that runs /review in sessions that do nothing else
)

// weekState is what a person has already done in the current week, which some stories depend on.
type weekState struct {
	week        int
	deployCheck bool // used /deploy-check
	jira        bool // called the Jira connector
}

// writeSession writes one whole session that starts at start and may spend about budget.
func writeSession(o *Org, p *Person, r *rand.Rand, kind sessionKind, start time.Time, budget float64, ws *weekState) *session {
	day := int(start.Sub(o.Start) / (24 * time.Hour))
	st := o.Preset.Stories
	rel := o.release(p, day)
	s := &session{
		o: o, p: p, r: r, t: start, model: p.Model, planModel: p.Model,
		id:            uuid(r),
		kind:          kind,
		week:          day/7 + 1,
		ws:            ws,
		version:       version(rel),
		buggy:         o.buggy(rel),
		scripted:      p.Style.Scripted || kind == gaming,
		terminal:      p.Terminal,
		allowed:       map[string]bool{},
		costByModel:   map[string]float64{},
		tokensByModel: map[string]*[4]int{},
	}
	if kind == gaming {
		s.terminal = "non-interactive"
	}
	if st.ExpensiveModel > 0 && s.week >= st.ExpensiveModel && p.Team.Name == st.ExpensiveTeam {
		s.model = expensiveModel
	}
	s.tally.id, s.tally.kind, s.tally.week, s.tally.buggy = s.id, kind, s.week, s.buggy
	s.compactAt = s.nextCompaction()
	s.pickRepo()
	s.paths = p.pathsFor(s.repo, s.id)
	s.resource = s.resourceAttrs()

	s.loadInstructions()
	if st.Connector > 0 && s.week >= st.Connector {
		s.prefix += connectorTokens
		s.extra += connectorTokens
		if p.JiraWeeks[s.week] && !ws.jira && !s.scripted {
			ws.jira, s.callJira = true, true
		}
	}
	// Claude Code's own prefix is the same in every session, so the person's other sessions
	// usually have it in the cache already.
	s.main = conv{sent: s.prefix, cached: basePrefixTokens, last: s.t}
	s.first = s.firstCommand()
	s.tally.first = s.first
	s.editBoost = s.first == "deploy-check"

	style := p.Style
	requests := s.planRequests(budget * (1 - 0.6*style.Subagents) * 0.92)
	prompts := style.Prompts[0]
	if style.Prompts[1] > style.Prompts[0] {
		prompts = int(math.Round(logUniform(r, float64(style.Prompts[0]), float64(style.Prompts[1]))))
	}
	if s.scripted || kind == overnight {
		prompts = 1
	}
	prompts = max(1, min(prompts, requests))

	s.t = s.t.Add(ms(3000 + 25000*r.Float64()))
	for i, n := range split(r, requests, prompts) {
		if i > 0 && !s.scripted {
			s.t = s.t.Add(time.Duration(logNormal(r, float64(style.Think), 0.8)))
			s.maybeBuiltin()
		}
		s.prompt(i, n)
		if !s.scripted && r.Float64() < 0.02 {
			s.hook(s.t.Add(ms(400)), "ConfigChange",
				field{"prompt_id", s.promptID},
				field{"file_path", s.join(s.paths.cwd, ".claude", "settings.local.json")},
				field{"source", "local_settings"})
		}
	}

	s.t = s.t.Add(time.Duration(logNormal(r, float64(20*time.Second), 0.8)))
	s.hook(s.t, "SessionEnd", field{"prompt_id", s.promptID}, field{"reason", pick(r, endReasons)})
	s.writeMetrics(start)
	s.tally.end = s.last
	return s
}

// firstCommand is the command the session starts with: /review from the gaming script,
// /deploy-check for someone who has taken it up, /review in a team it was rolled out to, or none.
func (s *session) firstCommand() string {
	st := s.o.Preset.Stories
	switch {
	case s.kind == gaming:
		return "review"
	case s.scripted || s.kind == overnight:
		return ""
	case s.p.DeployCheckWeek > 0 && s.week >= s.p.DeployCheckWeek && (!s.ws.deployCheck || s.r.Float64() < deployCheckChance):
		s.ws.deployCheck = true
		return "deploy-check"
	case st.Review > 0 && s.week >= st.Review && !onTeam(st.NoReview, s.p.Team.Name) && s.r.Float64() < reviewChance:
		return "review"
	}
	return ""
}

// planRequests finds how many main-thread requests spend about the budget, from what one request
// costs on average at this session's context size. It plans at the person's own model and
// without the context the stories added, so that a story changes what the work costs and not how
// much of it there is.
func (s *session) planRequests(budget float64) int {
	price := Prices[s.planModel]
	style := s.p.Style
	add := style.Growth * math.Exp(0.8*0.8/2)
	out := style.Output * math.Exp(0.5)
	step := add + out
	prefix := s.prefix - s.extra
	span := float64(autoCompactTokens - prefix)
	cost := func(n int) float64 {
		avg := float64(prefix) + math.Min(float64(n)*step, span)/2
		return float64(n) * (avg*price.CacheRead + step*price.CacheWrite + out*price.Output + 20*price.Input)
	}
	lo, hi := 1, maxRequestsPerSession
	if cost(hi) <= budget {
		return hi
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if cost(mid) < budget {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// split shares n requests between k prompts, at least one each, unevenly.
func split(r *rand.Rand, n, k int) []int {
	out := make([]int, k)
	weights := make([]float64, k)
	total := 0.0
	for i := range weights {
		weights[i] = r.ExpFloat64()
		total += weights[i]
	}
	left := n - k
	given := 0
	for i := range out {
		extra := int(float64(left) * weights[i] / total)
		out[i] = 1 + extra
		given += extra
	}
	for i := 0; given < left; i = (i + 1) % k {
		out[i]++
		given++
	}
	return out
}

func (s *session) pickRepo() {
	x := s.r.Float64()
	var orgRepos []int
	ext := -1
	for i, rp := range s.p.Repos {
		if rp.Org {
			orgRepos = append(orgRepos, i)
		} else {
			ext = i
		}
	}
	switch {
	case x < 0.80 && len(orgRepos) > 0:
		s.repo = &s.p.Repos[orgRepos[s.r.IntN(len(orgRepos))]]
	case x < 0.92 && ext >= 0:
		s.repo = &s.p.Repos[ext]
	}
}

func (s *session) nextCompaction() int {
	return int(autoCompactTokens * (0.9 + 0.2*s.r.Float64()))
}

// ---- Instructions files ------------------------------------------------------------------

// loadInstructions loads what Claude Code loads at session start: the organization's managed
// CLAUDE.md, the person's own, the repository's, and, in the organization's repositories, the
// standards file the repository's CLAUDE.md imports.
func (s *session) loadInstructions() {
	r := s.r
	s.prefix = basePrefixTokens + 900 + 2500*len(s.p.MCPServers)
	next := func() time.Time { s.t = s.t.Add(ms(15 + 60*r.Float64())); return s.t }

	s.hook(next(), "InstructionsLoaded",
		field{"file_path", s.paths.managed}, field{"memory_type", "Managed"}, field{"load_reason", "session_start"})
	s.use(s.t, "instructions", "", "CLAUDE.md", false, false)
	if s.p.UserClaudeMD {
		s.hook(next(), "InstructionsLoaded",
			field{"file_path", s.join(s.paths.home, ".claude", "CLAUDE.md")},
			field{"memory_type", "User"}, field{"load_reason", "session_start"})
		s.use(s.t, "instructions", "", "CLAUDE.md", false, false)
		s.prefix += 1000 + r.IntN(3000)
	}
	if s.repo == nil || (!s.repo.Org && r.Float64() < 0.5) {
		return
	}
	project := s.join(s.paths.cwd, "CLAUDE.md")
	s.hook(next(), "InstructionsLoaded",
		field{"file_path", project}, field{"memory_type", "Project"}, field{"load_reason", "session_start"})
	s.use(s.t, "instructions", "", "CLAUDE.md", false, false)
	s.prefix += 1500 + r.IntN(4500)
	if s.repo.Org {
		st := s.o.Preset.Stories
		version := 2
		if st.InstructionsV3 > 0 && s.week >= st.InstructionsV3 && !onTeam(st.StayOnV2, s.p.Team.Name) {
			version = 3
		}
		name := fmt.Sprintf("example-standards-v%d.md", version)
		s.hook(next(), "InstructionsLoaded",
			field{"memory_type", "Project"}, field{"parent_file_path", project},
			field{"file_path", s.join(s.paths.cwd, name)}, field{"load_reason", "include"})
		s.use(s.t, "instructions", name, "other", true, false)
		s.prefix += 5000
		if version == 3 {
			s.prefix += instructionsV3Tokens
			s.extra += instructionsV3Tokens
		}
		s.tally.standards = version
	}
}

// loadOnRead loads, at most once a session each, a nested CLAUDE.md or a path-scoped rule that a
// file the session read or edited brought in.
func (s *session) loadOnRead(at time.Time) {
	if s.repo == nil || !s.repo.Org {
		return
	}
	if !s.nestedDone && s.r.Float64() < 0.02 {
		s.nestedDone = true
		s.hook(at.Add(ms(5)), "InstructionsLoaded",
			field{"prompt_id", s.promptID}, field{"file_path", s.join(s.paths.cwd, "service", "CLAUDE.md")},
			field{"memory_type", "Project"}, field{"load_reason", "nested_traversal"},
			field{"trigger_file_path", s.join(s.paths.cwd, "service", "made_up.go")})
		s.use(at.Add(ms(5)), "instructions", "", "CLAUDE.md", false, false)
		s.main.sent += 1500
	}
	if !s.ruleDone && s.r.Float64() < 0.015 {
		s.ruleDone = true
		s.hook(at.Add(ms(55)), "InstructionsLoaded",
			field{"prompt_id", s.promptID}, field{"file_path", s.join(s.paths.cwd, ".claude", "rules", "api.md")},
			field{"memory_type", "Project"}, field{"load_reason", "path_glob_match"},
			field{"globs", []string{"api/**"}},
			field{"trigger_file_path", s.join(s.paths.cwd, "api", "made_up.go")})
		s.use(at.Add(ms(55)), "instructions", "", "rule", false, false)
		s.main.sent += 800
	}
}

// ---- Prompts -----------------------------------------------------------------------------

// maybeBuiltin runs, now and then, one of Claude Code's built-in commands between two prompts:
// /model, /compact or /context. Each is a prompt of its own in OTel, with no hook of its own.
func (s *session) maybeBuiltin() {
	x := s.r.Float64()
	switch {
	case x < 0.03:
		s.builtinPrompt("model")
		s.switchModel()
	case x < 0.05 && s.main.sent+s.main.out > 50000:
		s.builtinPrompt("compact")
		s.compact("manual")
	case x < 0.06:
		s.builtinPrompt("context")
	}
	s.t = s.t.Add(ms(2000 + 20000*s.r.Float64()))
}

func (s *session) builtinPrompt(name string) {
	s.promptID = uuid(s.r)
	s.skill = ""
	s.log(s.t, "user_prompt", "prompt_length", itoa(len(name)+1), "command_name", name, "command_source", "builtin")
}

// prompt is the session's i-th prompt, answered with n main-thread requests.
func (s *session) prompt(i, n int) {
	r := s.r
	s.promptID = uuid(r)
	s.usedInPrompt = map[string]bool{}
	command, source := s.pickCommand()
	if i == 0 && s.first != "" {
		command, source = s.first, "projectSettings"
	}
	builtin := source == "builtin"
	s.skill = ""
	if command != "" && !builtin {
		s.skill = command
		s.usedInPrompt["skill|"+command] = true
	}
	if command == "" {
		// A skill the model chooses itself: every request in the prompt names it, and Claude Code
		// sends no expansion for it.
		st := s.o.Preset.Stories
		switch x := r.Float64(); {
		case st.Connector > 0 && s.week >= st.Connector && x < apiGuidelinesChance:
			s.skill = "api-guidelines"
		case st.PerfBudget > 0 && s.week >= st.PerfBudget && onTeam(st.PerfBudgetTeams, s.p.Team.Name) &&
			x > 1-perfBudgetChance:
			s.skill = "perf-budget"
		}
	}

	length := int(math.Max(5, math.Min(6000, logNormal(r, 140, 1))))
	if command != "" {
		length = int(math.Min(8000, logNormal(r, 900, 0.6)))
		s.hook(s.t, "UserPromptExpansion",
			field{"prompt_id", s.promptID}, field{"permission_mode", s.p.Mode},
			field{"expansion_type", "slash_command"}, field{"command_name", command},
			field{"command_args", filler("made-up arguments ", r.IntN(40))},
			field{"command_source", source}, field{"prompt", filler("made-up command text ", length)})
		s.use(s.t, "skill", command, "", orgName(command), builtin)
	}
	s.hook(s.t.Add(ms(3)), "UserPromptSubmit",
		field{"prompt_id", s.promptID}, field{"permission_mode", s.p.Mode},
		field{"prompt", filler("made-up prompt text ", length)})
	switch {
	case command == "":
		s.log(s.t, "user_prompt", "prompt_length", itoa(length))
	case builtin:
		s.log(s.t, "user_prompt", "prompt_length", itoa(length), "command_name", command, "command_source", source)
	default:
		// Claude Code reports a custom command as "custom" here, though it names it on every
		// request (observed on 2.1.281).
		s.log(s.t, "user_prompt", "prompt_length", itoa(length), "command_name", "custom", "command_source", source)
	}
	if !builtin {
		s.tally.prompts++
	}
	s.main.sent += length / 4

	subagent := r.Float64() < s.p.Style.Subagents
	flaky := -1
	if st := s.o.Preset.Stories; s.p.Team.Name == st.FlakyTeam && !s.scripted && n > 1 && r.Float64() < flakyChance {
		flaky = r.IntN(n - 1)
	}
	for k := range n {
		if s.main.sent+s.main.out > s.compactAt {
			s.compact("auto")
		}
		s.request(&s.main, "repl_main_thread", s.model, "", s.add(), s.output(), 0)
		switch {
		case k == n-1:
		case subagent && k == 0:
			s.subagent()
		case s.callJira:
			s.callJira = false
			s.callTool(nil, "mcp__jira__"+mcpOperations[r.IntN(len(mcpOperations))], "")
		case k == flaky:
			s.retryLoop()
		default:
			s.toolCall(nil)
		}
	}

	// Claude Code's prompt-suggestion helper, after some turns: it fires a SubagentStop with no
	// start, then a request of its own (observed on 2.1.281).
	if !s.scripted && r.Float64() < 0.2 {
		s.t = s.t.Add(ms(200))
		s.subagentStop(s.t, agentID(r), "")
		helper := conv{sent: s.main.sent + s.main.out, cached: s.main.sent, last: s.main.last}
		s.t = s.t.Add(ms(1000 + 2000*r.Float64()))
		s.request(&helper, "prompt_suggestion", s.model, "", 40, 10+r.IntN(30), 0)
	}
}

// pickCommand decides whether a prompt is a slash command, and which.
func (s *session) pickCommand() (name, source string) {
	r := s.r
	if s.scripted || s.kind == overnight {
		return "", ""
	}
	day := int(s.t.Sub(s.o.Start).Hours() / 24)
	switch x := r.Float64(); {
	case s.p.JoinDay > 0 && day-s.p.JoinDay < 14 && x < 0.2:
		return "onboarding", "projectSettings"
	case len(s.p.Commands) > 0 && x < 0.3:
		if r.Float64() < 0.35 {
			c := s.p.Commands[r.IntN(len(s.p.Commands))]
			for _, shared := range sharedCommands {
				if shared.Name == c {
					return c, "projectSettings"
				}
			}
			return c, "userSettings"
		}
	case x > 0.997:
		return "onboarding", "projectSettings"
	case x > 0.992:
		return "init", "builtin"
	}
	return "", ""
}

func orgName(name string) bool { return orgPattern.MatchString(name) }

// ---- Requests and tools ------------------------------------------------------------------

func (s *session) add() int { return int(math.Max(50, logNormal(s.r, s.p.Style.Growth, 0.8))) }
func (s *session) output() int {
	return int(math.Max(5, math.Min(16000, logNormal(s.r, s.p.Style.Output, 1))))
}

// request sends one API request on conversation c, adding add tokens to it and getting out
// tokens back, and moves the session's clock to its end. A zero duration is worked out from the
// output.
func (s *session) request(c *conv, query, model, agent string, add, out int, duration time.Duration) {
	r := s.r
	price := Prices[model]
	sent := c.sent + c.out + add
	read := min(c.cached, sent)
	if s.t.Sub(c.last) > time.Hour {
		read = 0
	}
	if s.buggy && r.Float64() < bugColdChance {
		// What reading it normally would have saved: the tokens written instead, at the
		// difference between the two prices.
		cold := min(read, basePrefixTokens)
		s.tally.bugCost += float64(read-cold) * (price.CacheWrite - price.CacheRead)
		read = cold
	}
	input := min(1+r.IntN(40), sent-read)
	write := sent - read - input
	cost := float64(input)*price.Input + float64(read)*price.CacheRead +
		float64(write)*price.CacheWrite + float64(out)*price.Output
	cost = math.Round(cost*1e10) / 1e10

	ttft := 500 + logNormal(r, 900, 0.5)
	if duration == 0 {
		duration = ms(ttft + float64(out)*1000/70)
	}
	s.t = s.t.Add(duration)
	pairs := []string{
		"model", model, "effort", s.p.Effort, "query_source", query,
		"input_tokens", itoa(input), "output_tokens", itoa(out),
		"cache_read_tokens", itoa(read), "cache_creation_tokens", itoa(write),
		"cost_usd", ftoa(cost), "cost_usd_micros", itoa(int(math.Round(cost * 1e6))),
		"duration_ms", itoa(int(duration / time.Millisecond)), "ttft_ms", itoa(int(ttft)),
	}
	if s.skill != "" {
		pairs = append(pairs, "skill.name", s.skill)
	}
	if agent != "" {
		pairs = append(pairs, "agent.name", agent)
	}
	s.log(s.t, "api_request", pairs...)
	if s.skill != "" && !s.usedInPrompt["skill|"+s.skill] {
		s.usedInPrompt["skill|"+s.skill] = true
		s.use(s.t, "skill", s.skill, "", orgName(s.skill), false)
	}

	c.sent, c.out, c.cached, c.last = sent, out, sent, s.t
	s.tally.requests++
	s.tally.cost += cost
	if query == "repl_main_thread" {
		if s.tally.mainRequests == 0 {
			s.tally.startContext = sent
		}
		s.tally.mainRequests++
	}
	if s.inRetry {
		s.tally.retryRequests++
		s.tally.retryCost += cost
	}
	s.costByModel[model] += cost
	tk := s.tokensByModel[model]
	if tk == nil {
		tk = &[4]int{}
		s.tokensByModel[model] = tk
	}
	tk[0] += input
	tk[1] += out
	tk[2] += read
	tk[3] += write
}

// subagentRun is the subagent a tool call is made in, if any.
type subagentRun struct {
	id, kind string
}

var subagentTools = []weighted{{"Read", 0.5}, {"Grep", 0.3}, {"Glob", 0.15}, {"Bash", 0.05}}
var mcpOperations = []string{"search", "get_issue", "list", "query"}

// The tools a request in an overnight loop ends in: mostly running the tests and fixing what
// they find.
var overnightMix = []weighted{{"Bash", 0.45}, {"Read", 0.25}, {"Edit", 0.2}, {"Grep", 0.1}}

// toolCall runs one tool the last request asked for, with the permission prompt it needs.
func (s *session) toolCall(in *subagentRun) { s.callTool(in, "", "") }

// callTool runs tool, or one picked the way the session picks them when tool is empty. outcome
// is "fail" or "pass" for a test run whose result is known, and empty otherwise.
func (s *session) callTool(in *subagentRun, tool, outcome string) {
	r := s.r
	switch {
	case tool != "":
	case in != nil:
		tool = pick(r, subagentTools)
	case s.kind == overnight:
		tool = pick(r, overnightMix)
	case s.editBoost:
		tool = pick(r, boosted(toolMix, deployCheckEditBoost, "Edit", "Write"))
	default:
		tool = pick(r, toolMix)
	}
	server := ""
	if tool == "mcp" {
		if len(s.p.MCPServers) == 0 {
			tool = "Read"
		} else {
			server = s.p.MCPServers[r.IntN(len(s.p.MCPServers))]
			tool = "mcp__" + server + "__" + mcpOperations[r.IntN(len(mcpOperations))]
		}
	} else if strings.HasPrefix(tool, "mcp__") {
		server = strings.Split(tool, "__")[1]
	}
	id := toolUseID(r)
	start := s.t.Add(ms(5 + 20*r.Float64()))

	if s.p.Mode == "auto" && tool == "Bash" && outcome == "" && r.Float64() < 0.01 {
		// Auto mode refuses the call: a rejection by configuration in OTel, PermissionDenied in
		// the hooks, and no result (observed on 2.1.290).
		s.log(start, "tool_decision", "tool_name", tool, "tool_use_id", id, "decision", "reject", "source", "config")
		s.hook(start.Add(ms(10)), "PermissionDenied",
			field{"prompt_id", s.promptID}, field{"permission_mode", s.p.Mode},
			field{"effort", obj{{"level", s.p.Effort}}}, field{"tool_name", tool},
			field{"tool_input", toolInput(s, tool)}, field{"tool_use_id", id},
			field{"reason", "made-up reason for the denial"})
		s.t = start.Add(ms(30))
		return
	}

	decided := start.Add(ms(3))
	decision, source := "accept", "config"
	firstSeen := decided
	if s.asks(tool) {
		// The prompt appears when Claude Code runs the PermissionRequest hooks, which it reports
		// itself as hook_execution_start; the answer is the next tool_decision.
		s.log(start, "hook_execution_start", "hook_event", "PermissionRequest", "hook_name", "PermissionRequest:"+tool)
		fields := []field{{"prompt_id", s.promptID}, {"permission_mode", s.p.Mode}, {"effort", obj{{"level", s.p.Effort}}}}
		if in != nil {
			fields = append(fields, field{"agent_id", in.id}, field{"agent_type", in.kind})
		}
		fields = append(fields, field{"tool_name", tool}, field{"tool_input", toolInput(s, tool)})
		if in == nil && server == "" {
			fields = append(fields, field{"permission_suggestions", []obj{{
				{"type", "addRules"}, {"behavior", "allow"}, {"destination", "session"},
				{"rules", []obj{{{"toolName", tool}, {"ruleContent", "made-up rule"}}}},
			}}})
		}
		s.hook(start.Add(ms(10)), "PermissionRequest", fields...)
		firstSeen = start.Add(ms(10))
		wait := math.Min(600000, logNormal(r, 5000, 0.9))
		decided = start.Add(ms(wait))
		source = pick(r, permissionAnswers)
		if source == "user_reject" {
			decision = "reject"
		}
		if source == "user_permanent" {
			s.allowed[tool] = true
		}
		s.tally.permissionPrompts++
		// As the view measures it: from the prompt to the answer, in whole milliseconds of each.
		s.tally.permissionWaitMs += decided.UnixMilli() - start.UnixMilli()
	}
	s.log(decided, "tool_decision", "tool_name", tool, "tool_use_id", id, "decision", decision, "source", source)
	if tool == "Edit" || tool == "Write" {
		s.tally.editProposals++
		if decision == "reject" {
			s.tally.editRejections++
		} else {
			s.tally.acceptedEdits++
		}
	}
	if server != "" && !s.usedInPrompt["mcp|"+server] {
		s.usedInPrompt["mcp|"+server] = true
		s.use(firstSeen, "mcp", server, "", orgName(server) || orgName("mcp__"+server+"__"), false)
	}
	if decision == "reject" {
		s.t = decided.Add(ms(20))
		return
	}
	took := toolDuration(r, tool)
	switch {
	case outcome != "":
		took = time.Duration(logNormal(r, float64(25*time.Second), 0.4)) // the whole test suite
	case s.kind == overnight && tool == "Bash":
		took = time.Duration(logNormal(r, float64(time.Minute), 0.6))
	}
	done := decided.Add(took)
	success := "true"
	switch {
	case outcome == "fail":
		success = "false"
		s.tally.failedRuns++
	case outcome == "pass":
	case r.Float64() < 0.06:
		success = "false"
	}
	s.log(done, "tool_result", "tool_name", tool, "tool_use_id", id, "success", success,
		"duration_ms", itoa(int(done.Sub(decided)/time.Millisecond)))
	if tool == "Read" || tool == "Edit" {
		s.loadOnRead(done)
	}
	s.t = done.Add(ms(20))
}

// retryLoop is the flaky team's test command failing: the model runs it, reads the failure, and
// runs it again, until it passes. Each rerun is a request more than the prompt was planned with.
func (s *session) retryLoop() {
	runs := 3 + s.r.IntN(5)
	s.tally.retryLoops++
	for i := range runs {
		if i == runs-1 {
			s.callTool(nil, "Bash", "pass")
			return
		}
		s.callTool(nil, "Bash", "fail")
		if s.main.sent+s.main.out > s.compactAt {
			s.compact("auto")
		}
		s.inRetry = true
		s.request(&s.main, "repl_main_thread", s.model, "", 400+s.r.IntN(2500), s.output(), 0)
		s.inRetry = false
	}
}

// boosted is a mix with the named tools' weights multiplied by factor.
func boosted(mix []weighted, factor float64, tools ...string) []weighted {
	out := slices.Clone(mix)
	for i := range out {
		if slices.Contains(tools, out[i].Value) {
			out[i].Weight *= factor
		}
	}
	return out
}

// asks says whether a call to tool brings up a permission prompt for this person.
func (s *session) asks(tool string) bool {
	if s.scripted || s.p.Mode == "auto" || s.allowed[tool] {
		return false
	}
	r := s.r
	switch {
	case tool == "Edit" || tool == "Write":
		return s.p.Mode == "default" && r.Float64() < 0.85
	case tool == "Bash":
		return r.Float64() >= s.p.BashAllow
	case tool == "WebFetch" || strings.HasPrefix(tool, "mcp__"):
		return r.Float64() < 0.5
	}
	return false
}

func toolDuration(r *rand.Rand, tool string) time.Duration {
	switch {
	case tool == "Bash":
		return time.Duration(logNormal(r, float64(2*time.Second), 1.2))
	case tool == "WebFetch":
		return time.Duration(logNormal(r, float64(1500*time.Millisecond), 0.5))
	case strings.HasPrefix(tool, "mcp__"):
		return time.Duration(logNormal(r, float64(800*time.Millisecond), 0.6))
	}
	return time.Duration(logNormal(r, float64(80*time.Millisecond), 0.6))
}

func toolInput(s *session, tool string) obj {
	file := s.join(s.paths.cwd, "made_up.go")
	switch {
	case tool == "Edit":
		return obj{{"file_path", file}, {"old_string", "made-up old text"}, {"new_string", "made-up new text"}, {"replace_all", false}}
	case tool == "Write":
		return obj{{"file_path", file}, {"content", "made-up file content"}}
	case tool == "Bash":
		return obj{{"command", "made-up command"}, {"description", "made-up description"}}
	case tool == "WebFetch":
		return obj{{"url", "made-up address"}, {"prompt", "made-up question"}}
	}
	return obj{{"query", "made-up query"}}
}

// subagent starts one subagent, which makes its own requests and tool calls and reports back.
func (s *session) subagent() {
	r := s.r
	kind, builtin := s.pickSubagent()
	run := &subagentRun{id: agentID(r), kind: kind}
	taskID := toolUseID(r)
	start := s.t.Add(ms(10))
	s.log(start, "tool_decision", "tool_name", "Task", "tool_use_id", taskID, "decision", "accept", "source", "config")
	s.hook(start.Add(ms(5)), "SubagentStart",
		field{"prompt_id", s.promptID}, field{"agent_id", run.id}, field{"agent_type", kind})
	s.use(start.Add(ms(5)), "subagent", kind, "", orgName(kind), builtin)
	s.t = start.Add(ms(20))

	model := s.model
	if kind == "Explore" {
		model = helperModel
	}
	// A project's own subagent is reported as "custom" (observed on 2.1.281); how a built-in one is
	// reported has not been observed.
	query, agent := "agent:custom", "custom"
	if builtin {
		query, agent = "agent:builtin", kind
	}
	steps := 2 + r.IntN(6)
	if s.p.Style.Name == "subagent-heavy" {
		steps = 3 + r.IntN(14)
	}
	c := conv{sent: 9000, cached: 9000, last: s.t}
	skill := s.skill
	s.skill = ""
	for k := range steps {
		s.request(&c, query, model, agent, int(logNormal(r, 2000, 0.8)), int(logNormal(r, 300, 0.8)), 0)
		if k < steps-1 {
			s.toolCall(run)
		}
	}
	s.skill = skill
	s.subagentStop(s.t.Add(ms(10)), run.id, kind)
	s.log(s.t.Add(ms(15)), "tool_result", "tool_name", "Task", "tool_use_id", taskID, "success", "true",
		"duration_ms", itoa(int(s.t.Sub(start)/time.Millisecond)))
	s.t = s.t.Add(ms(30))
	s.main.sent += 600 + r.IntN(1500)
}

func (s *session) subagentStop(at time.Time, id, kind string) {
	s.hook(at, "SubagentStop",
		field{"prompt_id", s.promptID}, field{"permission_mode", s.p.Mode},
		field{"effort", obj{{"level", s.p.Effort}}}, field{"agent_id", id}, field{"agent_type", kind},
		field{"agent_transcript_path", s.join(strings.TrimSuffix(s.paths.transcript, ".jsonl"), "subagents", "agent.jsonl")},
		field{"last_assistant_message", "made-up reply"},
		field{"background_tasks", []any{}}, field{"session_crons", []any{}}, field{"stop_hook_active", false})
}

func (s *session) pickSubagent() (kind string, builtin bool) {
	if len(s.p.Subagents) > 0 && s.r.Float64() < 0.5 {
		return s.p.Subagents[s.r.IntN(len(s.p.Subagents))], false
	}
	return pick(s.r, builtinSubagents), true
}

// ---- Context: compaction and model switches ----------------------------------------------

// compact summarizes the conversation, on the person's /compact or automatically near the window.
func (s *session) compact(trigger string) {
	r := s.r
	s.tally.compactions++
	if trigger == "auto" {
		s.tally.autoCompactions++
	}
	before := s.main.sent + s.main.out
	s.hook(s.t.Add(ms(5)), "PreCompact",
		field{"prompt_id", s.promptID}, field{"trigger", trigger}, field{"custom_instructions", ""})
	summary := 3000 + r.IntN(6000)
	start := s.t
	s.request(&s.main, "compact", s.model, "", 300, summary, time.Duration(30000+40000*r.Float64())*time.Millisecond)
	s.log(s.t, "compaction", "trigger", trigger, "pre_tokens", itoa(before-s.prefix), "post_tokens", itoa(summary),
		"duration_ms", itoa(int(s.t.Sub(start)/time.Millisecond)), "success", "true")
	s.hook(s.t.Add(ms(10)), "PostCompact",
		field{"prompt_id", s.promptID}, field{"trigger", trigger}, field{"compact_summary", "made-up summary"})
	// /compact also fires a SubagentStop with no agent type and no start (observed on 2.1.281).
	s.subagentStop(s.t.Add(ms(20)), agentID(r), "")
	s.main = conv{sent: s.prefix, out: summary, cached: s.prefix, last: s.t}
	s.compactAt = s.nextCompaction()
	s.t = s.t.Add(ms(50))
}

// switchModel is /model to the other main model. The new model's cache holds only Claude Code's
// own prefix, so the next request writes most of the conversation again.
func (s *session) switchModel() {
	from := s.model
	to, alias := "claude-opus-5-5", "opus"
	if from == to {
		to, alias = "claude-sonnet-5-5", "sonnet"
	}
	context := 0
	if s.tally.requests > 0 {
		context = s.main.sent + s.main.out
	}
	s.log(s.t.Add(ms(5)), "hook_execution_start", "hook_event", "PostModelSwitch", "hook_name", "PostModelSwitch:"+to)
	s.hook(s.t.Add(ms(15)), "PostModelSwitch",
		field{"prompt_id", s.promptID}, field{"from_model", from}, field{"to_model", to},
		field{"requested_model", alias}, field{"source", "command"}, field{"pricing", "catalog"},
		field{"context_tokens", context}, field{"prompt_cache_warm", s.t.Sub(s.main.last) < time.Hour && context > 0},
		field{"cache_ttl", "1h"},
		field{"estimated_cache_write_usd", math.Round(float64(context)*Prices[to].CacheWrite*1e4) / 1e4})
	s.model = to
	s.main.cached = min(s.main.cached, basePrefixTokens)
}

// ---- Writing events ----------------------------------------------------------------------

func (s *session) hook(at time.Time, event string, fields ...field) {
	o := obj{
		{"session_id", s.id}, {"transcript_path", s.paths.transcript}, {"cwd", s.paths.cwd},
		{"scratchpad_dir", s.paths.scratchpad}, {"hook_event_name", event},
	}
	b, err := json.Marshal(append(o, fields...))
	if err != nil {
		panic(err) // every value above is a string, a number, a bool or an obj
	}
	s.hooks = append(s.hooks, hookEvent{at, b})
	s.tally.hooks++
	s.saw(at)
}

func (s *session) log(at time.Time, event string, pairs ...string) {
	s.seq++
	a := attrs(
		"user.id", s.p.deviceID, "session.id", s.id, "app.version", s.version,
		"organization.id", organizationID, "user.email", s.p.Email, "user.account_uuid", s.p.accountUUID,
		"terminal.type", s.terminal, "event.name", event,
		"event.timestamp", at.Format("2006-01-02T15:04:05.000Z"), "event.sequence", itoa(s.seq),
		"prompt.id", s.promptID,
	)
	a = append(a, attrs(pairs...)...)
	s.logs = append(s.logs, logRecord{nanos(at), nanos(at), value{"claude_code." + event}, a})
	s.tally.records++
	if s.tally.start.IsZero() || at.Before(s.tally.start) {
		s.tally.start = at
	}
	s.saw(at)
}

func (s *session) saw(at time.Time) {
	if at.After(s.last) {
		s.last = at
	}
}

func (s *session) use(at time.Time, kind, name, fileKind string, org, builtin bool) {
	s.tally.uses = append(s.tally.uses, use{at, kind, name, fileKind, org, builtin})
}

// writeMetrics writes the session's metrics as Claude Code exports them: deltas, the session
// counted once, cost and tokens by model.
func (s *session) writeMetrics(start time.Time) {
	r := s.r
	base := func(extra ...string) []kv {
		return attrs(append([]string{
			"user.id", s.p.deviceID, "session.id", s.id, "app.version", s.version,
			"organization.id", organizationID, "user.email", s.p.Email, "user.account_uuid", s.p.accountUUID,
			"terminal.type", s.terminal,
		}, extra...)...)
	}
	at := nanos(s.last)
	from := nanos(start)
	point := func(v float64, extra ...string) dataPoint {
		s.tally.points++
		return dataPoint{v, from, at, base(extra...)}
	}
	s.points = append(s.points, metric{"claude_code.session.count", "count", sum{deltaTemporality, true,
		[]dataPoint{point(1, "start_type", pick(r, []weighted{{"fresh", 0.82}, {"resume", 0.1}, {"continue", 0.08}}))}}})
	var cost, tokens []dataPoint
	for _, m := range sortedKeys(s.costByModel) {
		cost = append(cost, point(s.costByModel[m], "model", m))
		tk := s.tokensByModel[m]
		for i, typ := range []string{"input", "output", "cacheRead", "cacheCreation"} {
			tokens = append(tokens, point(float64(tk[i]), "model", m, "type", typ))
		}
	}
	if len(cost) > 0 {
		s.points = append(s.points,
			metric{"claude_code.cost.usage", "USD", sum{deltaTemporality, true, cost}},
			metric{"claude_code.token.usage", "tokens", sum{deltaTemporality, true, tokens}})
	}
}

func (s *session) resourceAttrs() []kv {
	osVersion, arch := "6.8.0-60-generic", "amd64"
	switch s.p.OS {
	case "darwin":
		osVersion, arch = "25.1.0", "arm64"
	case "windows":
		osVersion = "10.0.26200"
	}
	a := attrs(
		"service.name", "claude-code", "service.version", s.version, "os.type", s.p.OS,
		"os.version", osVersion, "host.arch", arch, "host.name", s.p.ID+"-laptop",
		"cardo.cohort", s.p.Team.Name,
	)
	if s.repo != nil {
		owner := "example-oss"
		if s.repo.Org {
			owner = s.p.Team.Name
		}
		a = append(a, attrs("vcs.repository.url.full", s.repo.URL, "vcs.owner.name", owner,
			"vcs.repository.name", s.repo.Name)...)
	}
	return a
}

// ---- Paths and identifiers ---------------------------------------------------------------

// pathsFor gives the paths a session's hook payloads carry. They are filler, as every path is:
// the collector drops them all. The transcript path in particular points into a folder named for
// what it is, never at where Claude Code keeps real transcripts: nothing in Cardo names that
// folder outside the importer the transcript rule (INV-1) allows.
func (p *Person) pathsFor(repo *Repo, sid string) paths {
	dir := "scratch"
	if repo != nil {
		dir = repo.Name
	}
	switch p.OS {
	case "windows":
		home := `C:\Users\` + p.ID
		slug := "C--Users-" + p.ID + "-src-" + dir
		return paths{
			sep: `\`, home: home, cwd: home + `\src\` + dir,
			transcript: home + `\made-up-transcripts\` + slug + `\` + sid + ".jsonl",
			scratchpad: home + `\AppData\Local\Temp\claude\` + slug + `\` + sid + `\scratchpad`,
			managed:    `C:\Program Files\ClaudeCode\CLAUDE.md`,
		}
	case "darwin":
		home := "/Users/" + p.ID
		slug := "-Users-" + p.ID + "-src-" + dir
		return paths{
			sep: "/", home: home, cwd: home + "/src/" + dir,
			transcript: home + "/made-up-transcripts/" + slug + "/" + sid + ".jsonl",
			scratchpad: "/private/tmp/claude-501/" + slug + "/" + sid + "/scratchpad",
			managed:    "/Library/Application Support/ClaudeCode/CLAUDE.md",
		}
	}
	home := "/home/" + p.ID
	slug := "-home-" + p.ID + "-src-" + dir
	return paths{
		sep: "/", home: home, cwd: home + "/src/" + dir,
		transcript: home + "/made-up-transcripts/" + slug + "/" + sid + ".jsonl",
		scratchpad: "/tmp/claude-1000/" + slug + "/" + sid + "/scratchpad",
		managed:    "/etc/claude-code/CLAUDE.md",
	}
}

func (s *session) join(parts ...string) string { return strings.Join(parts, s.paths.sep) }

func uuid(r *rand.Rand) string {
	a, b := r.Uint64(), r.Uint64()
	return fmt.Sprintf("%08x-%04x-4%03x-%04x-%012x", a>>32, (a>>16)&0xffff, a&0xfff,
		0x8000|(b>>48)&0x3fff, b&0xffffffffffff)
}

func toolUseID(r *rand.Rand) string {
	return fmt.Sprintf("toolu_01%016x%06x", r.Uint64(), r.Uint32()&0xffffff)
}

func agentID(r *rand.Rand) string { return fmt.Sprintf("a%017x", r.Uint64()) }

// filler is n characters of made-up text. Where a real payload carries a prompt, a reply or a
// command, a made-up one carries this.
func filler(text string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(text, n/len(text)+1)[:n]
}
