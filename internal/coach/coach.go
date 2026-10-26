// Package coach builds the engineer's own figures: this window against the one before it, plus
// suggestions that name a concrete fix. It never ranks people and never returns another person's
// rows (ADR-0032, ADR-0041).
package coach

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Window is one person's numbers for one period. All values are counts or sums; nothing here is a
// score.
type Window struct {
	Sessions     int     `json:"sessions"`
	Prompts      int     `json:"prompts"`
	CostUSD      float64 `json:"costUsd"`
	OutputTokens uint64  `json:"outputTokens"`
	PeakContext  int64   `json:"peakContext"`
	FailedTools  int     `json:"failedTools"`
	ToolCalls    int     `json:"toolCalls"`
	Edits        int     `json:"edits"`
	// ToolsObserved says the tool figures were looked for and found. It is false when the window
	// holds imported history and nothing else: a transcript carries no tool decisions, so a failure
	// count of zero would say "no failures" where the truth is "not looked at" (ADR-0050).
	ToolsObserved bool `json:"toolsObserved"`
}

// Artifact is one thing this person loaded, by name.
type Artifact struct {
	Name  string `json:"name"`
	Loads int    `json:"loads"`
}

// PeerArtifact is something the fleet has shown and this person has not used yet, with the number
// of people who use it. The count is part of the suggestion: "11 people here use /review" is
// evidence, and a bare name is not (ADR-0041).
type PeerArtifact struct {
	Name   string `json:"name"`
	People int    `json:"people"`
}

// Report is what the coach shows. Suggestions are sentences with a fix, never a grade.
type Report struct {
	Pseudonym   string     `json:"pseudonym"`
	From, To    time.Time  `json:"-"`
	PrevFrom    time.Time  `json:"-"`
	Now         Window     `json:"now"`
	Prev        Window     `json:"prev"`
	Artifacts   []Artifact `json:"artifacts"`
	Untried     []string   `json:"untried"`
	Suggestions []string   `json:"suggestions"`
}

// Names returns the artifact names, for the report's flat list.
func Names(arts []PeerArtifact) []string {
	out := make([]string, 0, len(arts))
	for _, a := range arts {
		out = append(out, a.Name)
	}
	return out
}

// Query reads the figures for one pseudonym. from/to are inclusive days.
//
// Service runs are left out: ADR-0036 keeps them out of anything about people, and the coach is
// about one person (ADR-0032).
func Query(ctx context.Context, endpoint, user, pass, pseudonym string, from, to time.Time) (Window, []Artifact, error) {
	var w Window
	q := func(sql string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
		if err != nil {
			return "", err
		}
		req.Header.Set("X-ClickHouse-User", user)
		req.Header.Set("X-ClickHouse-Key", pass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return string(body), nil
	}

	sql := fmt.Sprintf(`SELECT uniqExact(session_id), count(), round(sum(cost_usd), 4), sum(output_tokens),
		toInt64(round(ifNotFinite(quantile(0.5)(peak_context_tokens), 0))) FROM cardo.silver_turn
		WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s')
			AND session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service') FORMAT TSV`,
		pseudonym, from.Format("2006-01-02"), to.Format("2006-01-02"))
	row, err := q(sql)
	if err != nil {
		return w, nil, err
	}
	var cost float64
	if _, err := fmt.Sscanf(strings.TrimSpace(row), "%d\t%d\t%f\t%d\t%d", &w.Sessions, &w.Prompts, &cost, &w.OutputTokens, &w.PeakContext); err != nil && strings.TrimSpace(row) != "" {
		return w, nil, fmt.Errorf("turns row %q: %w", row, err)
	}
	w.CostUSD = cost

	sql = fmt.Sprintf(`SELECT countIf(ran = 1 AND succeeded = 0), countIf(ran = 1), countIf(is_edit = 1)
		FROM cardo.silver_tool_call WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s')
			AND session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service') FORMAT TSV`,
		pseudonym, from.Format("2006-01-02"), to.Format("2006-01-02"))
	row, err = q(sql)
	if err != nil {
		return w, nil, err
	}
	_, _ = fmt.Sscanf(strings.TrimSpace(row), "%d\t%d\t%d", &w.FailedTools, &w.ToolCalls, &w.Edits)

	// Were the tool figures looked for at all? A transcript carries no tool decisions, so a window
	// of imported history reports no failures because nothing was observed, not because nothing
	// failed. The cohort tells the two apart (ADR-0050).
	sql = fmt.Sprintf(`SELECT countIf(cohort != 'imported') FROM cardo.silver_turn
		WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s')
			AND session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service') FORMAT TSV`,
		pseudonym, from.Format("2006-01-02"), to.Format("2006-01-02"))
	row, err = q(sql)
	if err != nil {
		return w, nil, err
	}
	var liveSessions int
	_, _ = fmt.Sscanf(strings.TrimSpace(row), "%d", &liveSessions)
	w.ToolsObserved = liveSessions > 0

	sql = fmt.Sprintf(`SELECT name, count() FROM cardo.silver_artifact_load
		WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s')
		GROUP BY name ORDER BY count() DESC LIMIT 3 FORMAT TSV`,
		pseudonym, from.Format("2006-01-02"), to.Format("2006-01-02"))
	row, err = q(sql)
	if err != nil {
		return w, nil, err
	}
	var arts []Artifact
	for _, line := range strings.Split(strings.TrimSpace(row), "\n") {
		if line == "" {
			continue
		}
		var a Artifact
		if _, err := fmt.Sscanf(line, "%s\t%d", &a.Name, &a.Loads); err == nil {
			arts = append(arts, a)
		}
	}
	return w, arts, nil
}

// Suggest returns at most one suggestion, and it names only something the fleet has shown
// (ADR-0041). There is no catalog of patterns and tools, and no fixed tip: "compact earlier" and
// "one in five tool calls failed" were rules written by us, and the agreed rule is that the coach
// speaks about what colleagues have adopted.
//
// The suggestion carries the number of people who use it, because that count is the evidence.
func Suggest(untried []PeerArtifact) []string {
	if len(untried) == 0 {
		return nil
	}
	a := untried[0]
	return []string{fmt.Sprintf("%d people here use %s, and you haven't tried it yet.", a.People, a.Name)}
}

// Untried lists artifacts the fleet has shown and this pseudonym has not used, with the number of
// people who use each.
//
// The floor is the organization's minimum group size, read from cardo.settings_effective, not a
// literal: an organization may raise it (ADR-0029), and the coach follows it.
//
// Service runs are left out, and so are Claude Code's own built-ins: neither identifies a person,
// and neither is something an engineer can be told to adopt (ADR-0036, ADR-0041).
func Untried(ctx context.Context, endpoint, user, pass, pseudonym string, from, to time.Time) ([]PeerArtifact, error) {
	sql := fmt.Sprintf(`WITH fleet AS (
			SELECT name, uniqExact(pseudonym) AS people FROM cardo.silver_artifact_load
			WHERE day BETWEEN toDate('%s') AND toDate('%s') AND pseudonym IS NOT NULL
				AND is_builtin = 0
				AND session_id IN (SELECT session_id FROM cardo.silver_session WHERE actor_kind != 'service')
			GROUP BY name
			HAVING people >= (SELECT min_group_size FROM cardo.settings_effective)
		), mine AS (
			SELECT DISTINCT name FROM cardo.silver_artifact_load
			WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s')
		)
		SELECT name, people FROM fleet WHERE name NOT IN (SELECT name FROM mine)
		ORDER BY people DESC, name ASC LIMIT 3 FORMAT TSV`,
		from.Format("2006-01-02"), to.Format("2006-01-02"),
		pseudonym, from.Format("2006-01-02"), to.Format("2006-01-02"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out []PeerArtifact
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var a PeerArtifact
		if _, err := fmt.Sscanf(line, "%s	%d", &a.Name, &a.People); err != nil {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// Unidentified counts sessions in the window that have no pseudonym. They exist, but no view can
// attribute them: Claude Code omits user.email when the run authenticates with an API key, and the
// collector has nothing to hash. The coach says so instead of showing an unexplained empty page.
func Unidentified(ctx context.Context, endpoint, user, pass string, from, to time.Time) (int, error) {
	sql := fmt.Sprintf(`SELECT count() FROM cardo.silver_session
		WHERE day BETWEEN toDate('%s') AND toDate('%s')
		AND (pseudonym IS NULL OR pseudonym = '') FORMAT TSV`,
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(string(body)), "%d", &n)
	return n, nil
}
