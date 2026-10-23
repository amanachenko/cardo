package madeup

import "time"

// Every figure in this file is a placeholder: a plausible default, not a measurement (ADR-0051,
// decision 6). They set how the made-up organization's people spread, so that a chart can be
// seen with an organization's unevenness in it. Nothing seen on the made-up stack is evidence
// about Claude Code, engineers or organizations. Better defaults change here, and only here.

// Style is one way of working with Claude Code. People are modelled as a mix of styles rather
// than as one average engineer with some noise, because people's spend and session counts differ
// widely and do not move together: someone with a dozen long sessions a month can spend more than
// someone with a thousand short ones.
type Style struct {
	Name  string
	About string
	// Share is how many of every 60 people work this way.
	Share int
	// Spend and Sessions are monthly ranges, each drawn log-uniformly and independently of the
	// other. A session's cost is what is left when one is divided by the other.
	Spend    [2]float64
	Sessions [2]float64
	// ActiveDay is the chance of using Claude Code on a working day; Weekend on a weekend day.
	ActiveDay, Weekend float64
	// Prompts is the range of prompts in one session.
	Prompts [2]int
	// Growth is the median number of tokens one request adds to the conversation (tool results
	// and the model's own output); Output the median output tokens of one request.
	Growth, Output float64
	// Think is the median time between one prompt's answer and the next prompt.
	Think time.Duration
	// Subagents is the chance that a prompt starts a subagent.
	Subagents float64
	// Modes are the permission modes people of this style work in, with weights.
	Modes []weighted
	// Terminals are the terminal.type values people of this style send.
	Terminals []string
	// Scripted sessions are started by a script on the engineer's laptop (claude -p), one prompt
	// each, with every tool allowed in settings: no permission prompt ever appears.
	Scripted bool
}

type weighted struct {
	Value  string
	Weight float64
}

// Styles, in the order people are assigned to them.
var Styles = []*Style{
	{
		Name: "steady", Share: 22,
		About:     "an everyday user in an editor: a few sessions a day, a handful of prompts each, answering permission prompts",
		Spend:     [2]float64{150, 900},
		Sessions:  [2]float64{40, 150},
		ActiveDay: 0.85, Weekend: 0.05,
		Prompts: [2]int{2, 14}, Growth: 2200, Output: 450, Think: 90 * time.Second, Subagents: 0.08,
		Modes:     []weighted{{"default", 0.6}, {"acceptEdits", 0.25}, {"auto", 0.15}},
		Terminals: []string{"vscode", "vscode", "cursor", "iTerm.app", "WindowsTerminal"},
	},
	{
		Name: "long-runs", Share: 8,
		About:     "long autonomous runs: few sessions, each running for hours, in auto mode, compacting as it goes",
		Spend:     [2]float64{800, 4000},
		Sessions:  [2]float64{15, 60},
		ActiveDay: 0.8, Weekend: 0.15,
		Prompts: [2]int{3, 25}, Growth: 3200, Output: 900, Think: 4 * time.Minute, Subagents: 0.15,
		Modes:     []weighted{{"auto", 0.8}, {"acceptEdits", 0.2}},
		Terminals: []string{"iTerm.app", "tmux", "WindowsTerminal", "vscode"},
	},
	{
		Name: "many-short", Share: 6,
		About:     "many short sessions: a new session for every small question, so hundreds a month at a small cost each",
		Spend:     [2]float64{100, 1500},
		Sessions:  [2]float64{400, 2000},
		ActiveDay: 0.9, Weekend: 0.1,
		Prompts: [2]int{1, 3}, Growth: 1500, Output: 300, Think: 40 * time.Second, Subagents: 0.03,
		Modes:     []weighted{{"default", 0.5}, {"auto", 0.5}},
		Terminals: []string{"iTerm.app", "vscode", "WindowsTerminal", "jetbrains"},
	},
	{
		Name: "parallel", Share: 5,
		About:     "several sessions at once, one per worktree, in auto mode",
		Spend:     [2]float64{1000, 4000},
		Sessions:  [2]float64{150, 500},
		ActiveDay: 0.85, Weekend: 0.15,
		Prompts: [2]int{2, 12}, Growth: 2800, Output: 700, Think: 3 * time.Minute, Subagents: 0.12,
		Modes:     []weighted{{"auto", 0.85}, {"acceptEdits", 0.15}},
		Terminals: []string{"tmux", "iTerm.app", "WindowsTerminal"},
	},
	{
		Name: "scripted", Share: 4,
		About:     "Claude Code run by a script on the engineer's laptop: one prompt per session, every tool allowed in settings",
		Spend:     [2]float64{100, 2000},
		Sessions:  [2]float64{300, 1500},
		ActiveDay: 0.7, Weekend: 0.2,
		Prompts: [2]int{1, 1}, Growth: 1800, Output: 500, Think: 10 * time.Second, Subagents: 0.02,
		Modes:     []weighted{{"default", 1}},
		Terminals: []string{"non-interactive"},
		Scripted:  true,
	},
	{
		Name: "subagent-heavy", Share: 7,
		About:     "subagents and frameworks: most prompts hand work to subagents, built-in and the engineer's own",
		Spend:     [2]float64{400, 2500},
		Sessions:  [2]float64{30, 120},
		ActiveDay: 0.85, Weekend: 0.08,
		Prompts: [2]int{2, 12}, Growth: 2600, Output: 600, Think: 2 * time.Minute, Subagents: 0.55,
		Modes:     []weighted{{"auto", 0.5}, {"default", 0.3}, {"acceptEdits", 0.2}},
		Terminals: []string{"vscode", "iTerm.app", "cursor", "WindowsTerminal"},
	},
	{
		Name: "occasional", Share: 8,
		About:     "an occasional user: a day or two a week, a few sessions, mostly reading code",
		Spend:     [2]float64{100, 250},
		Sessions:  [2]float64{4, 20},
		ActiveDay: 0.3, Weekend: 0.02,
		Prompts: [2]int{1, 8}, Growth: 1800, Output: 350, Think: 2 * time.Minute, Subagents: 0.05,
		Modes:     []weighted{{"default", 0.8}, {"acceptEdits", 0.2}},
		Terminals: []string{"vscode", "jetbrains", "WindowsTerminal", "iTerm.app"},
	},
}

// Price is dollars per token. Claude Code writes its prompt cache with a one-hour lifetime
// (cache_ttl "1h" in every model switch seen), so a cache write is priced at the one-hour rate.
type Price struct {
	Input, Output, CacheWrite, CacheRead float64
}

func perMillion(in, out, write, read float64) Price {
	return Price{in / 1e6, out / 1e6, write / 1e6, read / 1e6}
}

// Prices, from Anthropic's list prices as cached on 2026-10-06. Placeholders: the made-up
// organization's cost_usd is computed from them, where Claude Code's is its own figure.
var Prices = map[string]Price{
	"claude-fable-5-1":  perMillion(10, 50, 20, 0.25),
	"claude-opus-5-5":   perMillion(4, 20, 8, 0.20),
	"claude-sonnet-5-5": perMillion(2, 10, 4, 0.20),
	"claude-haiku-5-5":  perMillion(0.10, 0.50, 0.20, 0.01),
}

// The models people work on by default, with weights. Subagents that only read run on Haiku.
var mainModels = []weighted{{"claude-sonnet-5-5", 0.55}, {"claude-opus-5-5", 0.45}}

const (
	helperModel = "claude-haiku-5-5"

	// The conversation's fixed prefix before any instructions file: Claude Code's system prompt
	// and its tools.
	basePrefixTokens = 17000
	// Claude Code compacts a conversation automatically when it nears the context window. The
	// window is not reported; this is where the made-up sessions compact, give or take 10%.
	autoCompactTokens = 160000
	// The most requests one session makes, however large its budget.
	maxRequestsPerSession = 5000
)

// Effort levels people set, with weights. Claude Code's own default is xhigh.
var effortLevels = []weighted{{"xhigh", 0.7}, {"high", 0.2}, {"medium", 0.1}}

// The tools a request ends in, for a person working interactively. A subagent is started
// separately, with the chance its style gives.
var toolMix = []weighted{
	{"Read", 0.30}, {"Edit", 0.18}, {"Bash", 0.20}, {"Grep", 0.12}, {"Glob", 0.06},
	{"Write", 0.04}, {"WebFetch", 0.02}, {"TodoWrite", 0.03}, {"mcp", 0.05},
}

// How a person answers a permission prompt: a "yes", a "yes, don't ask again", or a "no".
var permissionAnswers = []weighted{
	{"user_temporary", 0.70}, {"user_permanent", 0.18}, {"user_reject", 0.12},
}

// Why a session ended. prompt_input_exit is /exit or Ctrl-D, the reason seen in every real
// session so far.
var endReasons = []weighted{
	{"prompt_input_exit", 0.70}, {"clear", 0.15}, {"other", 0.13}, {"logout", 0.02},
}
