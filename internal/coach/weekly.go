package coach

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Week is one point on the sparkline: sessions, cost and failed tool calls for a Monday.
type Week struct {
	Week     time.Time `json:"week"`
	Sessions int       `json:"sessions"`
	CostUSD  float64   `json:"costUsd"`
	Failed   int       `json:"failed"`
}

// Weekly returns the last n weeks for one pseudonym, oldest first. Two queries, merged by week.
func Weekly(ctx context.Context, endpoint, user, pass, pseudonym string, weeks int, now time.Time) ([]Week, error) {
	if weeks < 2 {
		weeks = 2
	}
	from := now.AddDate(0, 0, -7*weeks)
	post := func(sql string) (string, error) {
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

	turns, err := post(fmt.Sprintf(`SELECT toMonday(day) AS w, uniqExact(session_id), round(sum(cost_usd), 2)
		FROM cardo.silver_turn_live WHERE pseudonym = '%s' AND day >= toDate('%s')
		GROUP BY w ORDER BY w FORMAT TSV`, pseudonym, from.Format("2006-01-02")))
	if err != nil {
		return nil, err
	}
	points := map[string]*Week{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(turns), "\n") {
		if line == "" {
			continue
		}
		var day string
		var sessions int
		var cost float64
		if _, err := fmt.Sscanf(line, "%s\t%d\t%f", &day, &sessions, &cost); err != nil {
			continue
		}
		p := &Week{Sessions: sessions, CostUSD: cost}
		if t, err := time.Parse("2006-01-02", day); err == nil {
			p.Week = t
		}
		points[day] = p
		order = append(order, day)
	}

	fails, err := post(fmt.Sprintf(`SELECT toMonday(day) AS w, countIf(ran = 1 AND succeeded = 0)
		FROM cardo.silver_tool_call_live WHERE pseudonym = '%s' AND day >= toDate('%s')
		GROUP BY w ORDER BY w FORMAT TSV`, pseudonym, from.Format("2006-01-02")))
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(fails), "\n") {
		if line == "" {
			continue
		}
		var day string
		var n int
		if _, err := fmt.Sscanf(line, "%s\t%d", &day, &n); err != nil {
			continue
		}
		if p, ok := points[day]; ok {
			p.Failed = n
		}
	}

	out := make([]Week, 0, len(order))
	for _, day := range order {
		out = append(out, *points[day])
	}
	return out, nil
}
