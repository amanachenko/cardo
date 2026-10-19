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
	Blocked      int     `json:"blocked"`
	FailedTools  int     `json:"failedTools"`
	ToolCalls    int     `json:"toolCalls"`
	Edits        int     `json:"edits"`
}

// Artifact is one thing this person loaded, by name.
type Artifact struct {
	Name  string `json:"name"`
	Loads int    `json:"loads"`
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

// Query reads the figures for one pseudonym. from/to are inclusive days.
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
		WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s') FORMAT TSV`,
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
		FROM cardo.silver_tool_call WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s') FORMAT TSV`,
		pseudonym, from.Format("2006-01-02"), to.Format("2006-01-02"))
	row, err = q(sql)
	if err != nil {
		return w, nil, err
	}
	_, _ = fmt.Sscanf(strings.TrimSpace(row), "%d\t%d\t%d", &w.FailedTools, &w.ToolCalls, &w.Edits)

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

// Suggest turns a comparison into at most three sentences, each with a fix. It is pure so the rules
// can be tested without a database.
func Suggest(now, prev Window, untried []string) []string {
	var out []string
	if now.ToolCalls > 0 {
		rate := float64(now.FailedTools) / float64(now.ToolCalls)
		if rate >= 0.2 && now.FailedTools >= 5 {
			out = append(out, fmt.Sprintf("About one in five tool calls failed (%d of %d). Most failures are arguments or paths the tool refused; retrying the same call rarely helps.", now.FailedTools, now.ToolCalls))
		}
	}
	if prev.PeakContext > 0 && now.PeakContext > prev.PeakContext*5/4 {
		out = append(out, fmt.Sprintf("Your peak context grew from %d to %d tokens. Compact or split the task earlier; long sessions cost more per accepted edit.", prev.PeakContext, now.PeakContext))
	}
	if prev.Edits > 0 && now.Edits > 0 && now.CostUSD > 0 && prev.CostUSD > 0 {
		before := prev.CostUSD / float64(prev.Edits)
		after := now.CostUSD / float64(now.Edits)
		if after > before*1.3 {
			out = append(out, fmt.Sprintf("Cost per accepted edit rose from $%.2f to $%.2f. Check whether the extra turns were exploration or repeated context.", before, after))
		}
	}
	for _, name := range untried {
		out = append(out, fmt.Sprintf("Peers use %s and you have not tried it yet. Its page shows what they do with it.", name))
		if len(out) == 3 {
			break
		}
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// Untried lists artifacts at least five peers used that this pseudonym did not. The floor is the
// same as everywhere else: five people, never fewer.
func Untried(ctx context.Context, endpoint, user, pass, pseudonym string, from, to time.Time) ([]string, error) {
	sql := fmt.Sprintf(`WITH fleet AS (
			SELECT name FROM cardo.silver_artifact_load
			WHERE day BETWEEN toDate('%s') AND toDate('%s') AND pseudonym IS NOT NULL
			GROUP BY name HAVING uniqExact(pseudonym) >= 5
		), mine AS (
			SELECT DISTINCT name FROM cardo.silver_artifact_load
			WHERE pseudonym = '%s' AND day BETWEEN toDate('%s') AND toDate('%s')
		)
		SELECT name FROM fleet WHERE name NOT IN (SELECT name FROM mine) ORDER BY name LIMIT 3 FORMAT TSV`,
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
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
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
