// Package console implements the Claude Code Analytics API adapter (ADR-0021).
//
// Endpoint: GET /v1/organizations/usage_report/claude_code
// Credential: an Admin API key, provisioned by an organization admin.
//
// Every field name here comes from documentation, not from an observed response — see
// docs/research/2026-09-23-admin-analytics-apis.md, section 6. The adapter is therefore written to
// tolerate drift: unmodelled fields are reported rather than dropped, and the verbatim payload is
// what reaches storage.
package console

import "time"

// page is one response from the endpoint.
type page struct {
	Data     []record `json:"data"`
	HasMore  bool     `json:"has_more"`
	NextPage *string  `json:"next_page"`
}

// record is one actor's activity for one day.
type record struct {
	Date         time.Time   `json:"date"`
	Actor        actor       `json:"actor"`
	OrgID        string      `json:"organization_id"`
	CustomerType string      `json:"customer_type"`
	TerminalType string      `json:"terminal_type"`
	CoreMetrics  coreMetrics `json:"core_metrics"`
	ToolActions  toolActions `json:"tool_actions"`
	ModelUsage   []modelUse  `json:"model_breakdown"`
}

// actor is a discriminated union. Type is "user_actor" (carrying EmailAddress, the documented
// common case for OAuth authentication) or "api_actor" (carrying APIKeyName).
type actor struct {
	Type         string `json:"type"`
	EmailAddress string `json:"email_address,omitempty"`
	APIKeyName   string `json:"api_key_name,omitempty"`
}

// identifier returns the raw actor identity and the JSON path it was found at, so the scrubber can
// remove exactly that field without knowing this payload's shape.
func (a actor) identifier() (id string, path []string) {
	switch {
	case a.EmailAddress != "":
		return a.EmailAddress, []string{"actor", "email_address"}
	case a.APIKeyName != "":
		return a.APIKeyName, []string{"actor", "api_key_name"}
	default:
		return "", nil
	}
}

type coreMetrics struct {
	NumSessions  int         `json:"num_sessions"`
	LinesOfCode  linesOfCode `json:"lines_of_code"`
	Commits      int         `json:"commits_by_claude_code"`
	PullRequests int         `json:"pull_requests_by_claude_code"`
}

type linesOfCode struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

type toolActions struct {
	EditTool         decision `json:"edit_tool"`
	MultiEditTool    decision `json:"multi_edit_tool"`
	WriteTool        decision `json:"write_tool"`
	NotebookEditTool decision `json:"notebook_edit_tool"`
}

// decision is the accept/reject pair for one tool. This is the closest thing the zero-install tier
// has to an outcome variable — the daily-aggregate cousin of the friction index (ADR-0008).
type decision struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

type modelUse struct {
	Model  string `json:"model"`
	Tokens tokens `json:"tokens"`
	Cost   cost   `json:"estimated_cost"`
}

type tokens struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
}

// cost carries an amount in CENTS, not dollars.
//
// This is documented and easy to miss; reading it as dollars makes every cost figure on every
// dashboard 100x too large. The field is named AmountCents rather than Amount so that a reader
// cannot make that mistake without ignoring the name.
type cost struct {
	Currency    string `json:"currency"`
	AmountCents int64  `json:"amount"`
}

// knownPaths is every JSON path this adapter models. Anything in a response and not in this set is
// reported as drift (ADR-0014) rather than silently discarded.
var knownPaths = map[string]bool{
	"date": true, "organization_id": true, "customer_type": true, "terminal_type": true,
	"actor": true, "actor.type": true, "actor.email_address": true, "actor.api_key_name": true,
	"core_metrics":                              true,
	"core_metrics.num_sessions":                 true,
	"core_metrics.lines_of_code":                true,
	"core_metrics.lines_of_code.added":          true,
	"core_metrics.lines_of_code.removed":        true,
	"core_metrics.commits_by_claude_code":       true,
	"core_metrics.pull_requests_by_claude_code": true,
	"tool_actions":                              true,
	"tool_actions.edit_tool":                    true,
	"tool_actions.edit_tool.accepted":           true,
	"tool_actions.edit_tool.rejected":           true,
	"tool_actions.multi_edit_tool":              true,
	"tool_actions.multi_edit_tool.accepted":     true,
	"tool_actions.multi_edit_tool.rejected":     true,
	"tool_actions.write_tool":                   true,
	"tool_actions.write_tool.accepted":          true,
	"tool_actions.write_tool.rejected":          true,
	"tool_actions.notebook_edit_tool":           true,
	"tool_actions.notebook_edit_tool.accepted":  true,
	"tool_actions.notebook_edit_tool.rejected":  true,
	"model_breakdown":                           true,
	"model_breakdown.model":                     true,
	"model_breakdown.tokens":                    true,
	"model_breakdown.tokens.input":              true,
	"model_breakdown.tokens.output":             true,
	"model_breakdown.tokens.cache_read":         true,
	"model_breakdown.tokens.cache_creation":     true,
	"model_breakdown.estimated_cost":            true,
	"model_breakdown.estimated_cost.currency":   true,
	"model_breakdown.estimated_cost.amount":     true,
}
