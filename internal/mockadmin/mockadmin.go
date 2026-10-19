// Package mockadmin serves the Claude Code Analytics API shape for local verification. It is the
// stand-in the poller can talk to when no Admin API key exists yet (ADR-0047). It never ships in a
// release path: the poller only accepts a base URL on loopback.
package mockadmin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config describes the dataset the stand-in returns.
type Config struct {
	Days    int // consecutive days, ending yesterday
	Users   int // user_actor records per day
	APIKeys int // api_actor records per day
	Unknown int // records with an undocumented actor type, to exercise the drift check
	OrgID   string
	Now     time.Time
}

func (c Config) withDefaults() Config {
	if c.Days <= 0 {
		c.Days = 2
	}
	if c.Users < 0 {
		c.Users = 2
	}
	if c.OrgID == "" {
		c.OrgID = "00000000-0000-4000-8000-00000000cafe"
	}
	if c.Now.IsZero() {
		c.Now = time.Now().UTC()
	}
	return c
}

// Handler returns the stand-in endpoint.
func Handler(cfg Config) http.Handler {
	cfg = cfg.withDefaults()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/organizations/usage_report/claude_code", func(w http.ResponseWriter, r *http.Request) {
		day := r.URL.Query().Get("starting_at")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      onDay(dataset(cfg), day),
			"has_more":  false,
			"next_page": nil,
		})
	})
	return mux
}

// onDay keeps only the records for the requested day, as the real endpoint does.
func onDay(rows []map[string]any, day string) []map[string]any {
	if day == "" {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if strings.HasPrefix(r["date"].(string), day) {
			out = append(out, r)
		}
	}
	return out
}

func dataset(cfg Config) []map[string]any {
	day := cfg.Now.Truncate(24*time.Hour).AddDate(0, 0, -cfg.Days)
	var out []map[string]any
	for d := 0; d < cfg.Days; d++ {
		date := day.AddDate(0, 0, d).Format(time.RFC3339)
		for i := 0; i < cfg.Users; i++ {
			out = append(out, record(date, cfg.OrgID, "user_actor", map[string]any{
				"email_address": fmt.Sprintf("engineer%d@example.com", i+1),
			}, i))
		}
		for i := 0; i < cfg.APIKeys; i++ {
			out = append(out, record(date, cfg.OrgID, "api_actor", map[string]any{
				"api_key_name": fmt.Sprintf("developer-key-%d", i+1),
			}, i+10))
		}
		for i := 0; i < cfg.Unknown; i++ {
			out = append(out, record(date, cfg.OrgID, "service_account_actor", map[string]any{
				"api_key_name": fmt.Sprintf("unknown-key-%d", i+1),
			}, i+20))
		}
	}
	return out
}

func record(date, org, actorType string, identity map[string]any, seed int) map[string]any {
	actor := map[string]any{"type": actorType}
	for k, v := range identity {
		actor[k] = v
	}
	return map[string]any{
		"date":            date,
		"actor":           actor,
		"organization_id": org,
		"customer_type":   "api",
		"terminal_type":   "vscode",
		"core_metrics": map[string]any{
			"num_sessions": 3 + seed%4,
			"lines_of_code": map[string]any{
				"added":   120 + seed*7,
				"removed": 30 + seed*3,
			},
			"commits_by_claude_code":       2 + seed%3,
			"pull_requests_by_claude_code": seed % 2,
		},
		"tool_actions": map[string]any{
			"edit_tool":          map[string]any{"accepted": 40 + seed, "rejected": 5 + seed%4},
			"multi_edit_tool":    map[string]any{"accepted": 12 + seed, "rejected": 1},
			"write_tool":         map[string]any{"accepted": 6, "rejected": 0},
			"notebook_edit_tool": map[string]any{"accepted": 0, "rejected": 0},
		},
		"model_breakdown": []map[string]any{{
			"model": "claude-sonnet-4-5",
			"tokens": map[string]any{
				"input":          1000 + seed*10,
				"output":         400 + seed*5,
				"cache_read":     200,
				"cache_creation": 50,
			},
			"estimated_cost": map[string]any{"currency": "USD", "amount": 120 + seed*3},
		}},
	}
}
