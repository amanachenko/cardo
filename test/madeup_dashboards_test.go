package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// How the dashboards answer on the made-up organization (ADR-0051): every query each dashboard
// sends, through the made-up stack's Grafana as a browser opening the dashboard sends it, over the
// dashboard's own last thirty days and over the whole history. A query that fails fails the test;
// one slower than slowQuery does too, because nobody reviews a dashboard that takes a minute to
// draw, and nobody keeps opening one.
//
// It reads only the made-up stack, after the views check's own guards: scripts/made-up.sh check
// sets CARDO_MADEUP_GRAFANA_URL and the admin password beside CARDO_MADEUP_URL.

// slowQuery is the longest any one of a dashboard's queries may take.
const slowQuery = 10 * time.Second

// A browser opens at most six connections to one host, so a dashboard's panels are answered six
// at a time.
const browserConnections = 6

type grafanaTarget struct {
	where string
	body  map[string]any
}

type answer struct {
	where string
	took  time.Duration
	rows  int
	err   string
}

func TestMadeUpDashboardsAnswer(t *testing.T) {
	c := madeUpStack(t)
	raw := os.Getenv("CARDO_MADEUP_GRAFANA_URL")
	if raw == "" {
		t.Skip("set CARDO_MADEUP_GRAFANA_URL to the made-up stack's Grafana to time its dashboards " +
			"(scripts/made-up.sh check)")
	}
	m := regexp.MustCompile(`"(127\.0\.0\.1:\d+):3000"`).FindStringSubmatch(madeUpFile(t, "docker-compose.made-up.yml"))
	if m == nil {
		t.Fatal("docker-compose.made-up.yml publishes no Grafana port")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host != m[1] || strings.Trim(u.Path, "/") != "" {
		t.Fatalf("CARDO_MADEUP_GRAFANA_URL is %q; this test reads only the made-up stack's Grafana, http://%s", raw, m[1])
	}
	user := os.Getenv("CARDO_MADEUP_GRAFANA_USER")
	if user == "" {
		user = "admin"
	}
	password := os.Getenv("CARDO_MADEUP_GRAFANA_PASSWORD")

	first, err := time.Parse(time.DateOnly, c.key.FirstDay)
	if err != nil {
		t.Fatal(err)
	}
	last, err := time.Parse(time.DateOnly, c.key.LastDay)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ranges := []struct {
		name     string
		from, to time.Time
	}{
		{"as opened, the last 30 days", now.AddDate(0, 0, -30), now},
		{"the whole history", first, last.AddDate(0, 0, 1)},
	}
	t.Logf("The made-up organization: the %s preset, %d people, %s to %s.",
		c.key.Preset, len(c.key.People), c.key.FirstDay, c.key.LastDay)

	client := &http.Client{Timeout: 5 * time.Minute}
	ask := func(q grafanaTarget, from, to time.Time) answer {
		body, _ := json.Marshal(map[string]any{
			"queries": []any{q.body},
			"from":    strconv.FormatInt(from.UnixMilli(), 10),
			"to":      strconv.FormatInt(to.UnixMilli(), 10),
		})
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(raw, "/")+"/api/ds/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(user, password)
		began := time.Now()
		resp, err := client.Do(req)
		a := answer{where: q.where}
		if err != nil {
			a.err = err.Error()
			return a
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		a.took = time.Since(began)
		var r struct {
			Results map[string]struct {
				Error  string `json:"error"`
				Frames []struct {
					Data struct {
						Values [][]any `json:"values"`
					} `json:"data"`
				} `json:"frames"`
			} `json:"results"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(out, &r); err != nil || resp.StatusCode == http.StatusUnauthorized {
			a.err = fmt.Sprintf("answered %d: %.300s", resp.StatusCode, out)
			return a
		}
		for _, res := range r.Results {
			if res.Error != "" {
				a.err = res.Error
			}
			for _, f := range res.Frames {
				if len(f.Data.Values) > 0 {
					a.rows += len(f.Data.Values[0])
				}
			}
		}
		if a.err == "" && resp.StatusCode != http.StatusOK {
			a.err = fmt.Sprintf("answered %d: %s", resp.StatusCode, r.Message)
		}
		return a
	}

	dir := filepath.Join(repoRoot(t), "dashboards")
	for _, path := range jsonFiles(t, dir) {
		name := filepath.Base(path)
		targets := grafanaTargets(t, path)
		for _, rg := range ranges {
			t.Run(strings.TrimSuffix(name, ".json")+"/"+rg.name, func(t *testing.T) {
				answers := make([]answer, len(targets))
				began := time.Now()
				var wg sync.WaitGroup
				slots := make(chan struct{}, browserConnections)
				for i, q := range targets {
					wg.Add(1)
					go func() {
						defer wg.Done()
						slots <- struct{}{}
						defer func() { <-slots }()
						answers[i] = ask(q, rg.from, rg.to)
					}()
				}
				wg.Wait()
				whole := time.Since(began)

				slices.SortStableFunc(answers, func(a, b answer) int { return int(b.took - a.took) })
				var lines []string
				for _, a := range answers {
					lines = append(lines, fmt.Sprintf("%6.2f s %7d rows  %s", a.took.Seconds(), a.rows, a.where))
					switch {
					case a.err != "":
						t.Errorf("%s %s failed: %s", name, a.where, a.err)
					case a.took > slowQuery:
						t.Errorf("%s %s took %.1f s (%s); a query may take at most %s",
							name, a.where, a.took.Seconds(), rg.name, slowQuery)
					}
				}
				t.Logf("%s, %s: all %d queries answered in %.1f s, %d at a time. Slowest first:\n  %s",
					name, rg.name, len(answers), whole.Seconds(), browserConnections, strings.Join(lines, "\n  "))
			})
		}
	}
}

// grafanaTargets is every query a dashboard sends, each as the dashboard's own target: its SQL,
// its data source and its format, which is what Grafana's query endpoint takes.
func grafanaTargets(t *testing.T, path string) []grafanaTarget {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	qs, err := dashboardQueries(b)
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	bySQL := map[string]map[string]any{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e)
			}
		case map[string]any:
			if sql, ok := v["rawSql"].(string); ok {
				bySQL[sql] = v
			}
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(root)
	var out []grafanaTarget
	for _, q := range qs {
		target := bySQL[q.sql]
		if target == nil {
			t.Fatalf("%s %s has a query with no target to send it as", filepath.Base(path), q.where)
		}
		out = append(out, grafanaTarget{where: q.where, body: target})
	}
	if len(out) == 0 {
		t.Fatalf("%s sends no queries", filepath.Base(path))
	}
	return out
}
