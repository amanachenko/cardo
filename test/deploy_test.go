package test

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Every XML file under deploy/ must parse.
//
// This exists because of a real failure, not a hypothetical one: the ClickHouse quieting overlay
// shipped with "--" inside an XML comment, which the spec forbids. ClickHouse refused to start and
// crash-looped, and the only symptom was a container restarting. A config file that the server
// will reject is worth catching in CI rather than in a demo.
//
// Go's XML decoder rejects "--" in a comment for the same reason Poco does, so this test would
// have caught it.
func TestDeployXMLIsWellFormed(t *testing.T) {
	root := filepath.Join(repoRoot(t), "deploy")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("no deploy/ directory yet")
	}

	var checked int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".xml") {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		rel, _ := filepath.Rel(repoRoot(t), path)
		dec := xml.NewDecoder(f)
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s is not well-formed XML: %v\n"+
					"  ClickHouse refuses to start on a config it cannot parse, and the only\n"+
					"  symptom is a restarting container.", rel, err)
				return nil
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no XML files were checked; this test would pass vacuously")
	}
}

// INV-3, at the layer where it would actually be breached.
//
// The SQL test guards the views. This guards the dashboards, which is where someone adds "just one
// per-person panel" without touching any SQL at all. A dashboard may read only gold views, and may
// not name a pseudonym.
func TestINV3_DashboardsReadOnlyCohortViews(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "dashboards")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("no dashboards/ directory yet")
	}

	var queries int
	for _, path := range jsonFiles(t, dir) {
		rel, _ := filepath.Rel(repoRoot(t), path)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		qs, err := dashboardQueries(b)
		if err != nil {
			t.Errorf("%s is not valid JSON: %v", rel, err)
			continue
		}

		for _, q := range qs {
			queries++
			sql := strings.ToLower(q.sql)
			if strings.TrimSpace(sql) == "" {
				t.Errorf("INV-3: %s %s sends a query this test cannot read.\n"+
					"  A query variable's SQL is its query, or the query's rawSql. A query\n"+
					"  nobody checked can read whatever the data source's user can.", rel, q.where)
				continue
			}

			if strings.Contains(sql, "pseudonym") {
				t.Errorf("INV-3 violated: %s %s names a pseudonym:\n  %s\n"+
					"  Dashboards are cohort-only. A per-person row on a dashboard is a\n"+
					"  ranking a manager can sort, which is the thing this project promises\n"+
					"  not to build.", rel, q.where, q.sql)
			}
			for _, layer := range []string{"bronze_", "silver_"} {
				if strings.Contains(sql, layer) {
					t.Errorf("INV-3 violated: %s %s reads %s directly.\n"+
						"  Dashboards read gold views. Bronze and silver carry a pseudonym\n"+
						"  column, so reading them is one SELECT away from a leaderboard.",
						rel, q.where, strings.TrimSuffix(layer, "_"))
				}
			}
			if !strings.Contains(sql, "gold_") {
				t.Errorf("INV-3: %s %s reads no gold view:\n  %s", rel, q.where, q.sql)
			}
		}
	}
	if queries == 0 {
		t.Fatal("no dashboard queries were checked; this test would pass vacuously")
	}
}

// dashboardQuery is one SQL query a dashboard sends, and where in the dashboard it sits.
type dashboardQuery struct {
	where string // `panel "title"`, `variable "name"` or `annotation "name"`
	sql   string
}

// dashboardQueries returns every SQL query a dashboard sends: each rawSql at any depth, and the
// query of each query variable.
//
// The dashboard tests used to read only the top-level panels array. Grafana keeps the panels of a
// collapsed row inside the row, so collapsing a row hid its panels from both tests. A variable's
// query and an annotation's go through the same data source as a panel's, with the same rights.
// Walking the whole document finds them all, and finds the next place Grafana puts a query too.
//
// A query variable is returned even when its SQL cannot be found, with sql empty, so that a shape
// this function does not know fails the test instead of passing it.
func dashboardQueries(doc []byte) ([]dashboardQuery, error) {
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, err
	}
	var out []dashboardQuery
	var walk func(v any, kind, where string)
	walk = func(v any, kind, where string) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e, kind, where)
			}
		case map[string]any:
			for _, key := range []string{"title", "name"} {
				if s, ok := v[key].(string); ok && s != "" {
					where = fmt.Sprintf("%s %q", kind, s)
					break
				}
			}
			if kind == "variable" && v["type"] == "query" {
				out = append(out, dashboardQuery{where, variableSQL(v)})
				return
			}
			if sql, ok := v["rawSql"].(string); ok && strings.TrimSpace(sql) != "" {
				out = append(out, dashboardQuery{where, sql})
			}
			for _, key := range slices.Sorted(maps.Keys(v)) {
				next := kind
				switch key {
				case "panels":
					next = "panel"
				case "templating":
					next = "variable"
				case "annotations":
					next = "annotation"
				}
				walk(v[key], next, where)
			}
		}
	}
	walk(root, "dashboard", "")
	return out, nil
}

// variableSQL is a query variable's SQL. The ClickHouse plugin has stored it as a string and as an
// object carrying rawSql; definition is the text the variable editor shows.
func variableSQL(v map[string]any) string {
	switch q := v["query"].(type) {
	case string:
		if strings.TrimSpace(q) != "" {
			return q
		}
	case map[string]any:
		if s, ok := q["rawSql"].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	s, _ := v["definition"].(string)
	return s
}

// ADR-0008 makes the volume denominator a product constraint rather than styling: an acceptance
// rate without one rewards timidity, because a session that proposes nothing is never rejected and
// scores 100%. A panel that charts a rate must chart the volume it is measured over beside it.
//
// Each rate the gold views publish is paired with the column that is its volume. The friction
// index's three components are all here (ADR-0008): edit rejection, permission waits and
// compactions.
//
// The volume must be an output column, selected bare or named with AS. Appearing in the SQL is
// not enough: a panel computes its rate from the volume, as in
// `sum(edit_rejections) / nullIf(sum(edit_proposals), 0)`, so the name is there whether or not
// the volume is charted.
var rateVolumes = []struct {
	rate   string
	volume *regexp.Regexp
	name   string
}{
	{"acceptance_rate", outputColumn(`[a-z_]*proposals`), "proposals"},
	{"rejection_rate", outputColumn(`[a-z_]*proposals`), "edit_proposals"},
	{"compactions_per_session", outputColumn(`sessions`), "sessions"},
	{"permission_wait_s", outputColumn(`permission_answered`), "permission_answered"},
	{"permission_prompts_per_prompt", outputColumn(`prompts`), "prompts"},
	{"people_share", outputColumn(`active_people`), "active_people"},
}

// outputColumn matches a column in a lower-cased SELECT list: after AS, or bare between commas,
// and followed by a comma or FROM.
func outputColumn(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:\bas\s+|[\s,]|^)` + name + `\s*(?:,|\bfrom\b)`)
}

func TestADR0008_RatesAreChartedWithTheirDenominator(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "dashboards")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("no dashboards/ directory yet")
	}

	checked := map[string]int{}
	for _, path := range jsonFiles(t, dir) {
		rel, _ := filepath.Rel(repoRoot(t), path)
		b, _ := os.ReadFile(path)
		qs, err := dashboardQueries(b)
		if err != nil {
			continue // TestINV3_DashboardsReadOnlyCohortViews reports it
		}
		for _, q := range qs {
			sql := strings.ToLower(q.sql)
			for _, r := range rateVolumes {
				if !strings.Contains(sql, r.rate) {
					continue
				}
				checked[r.rate]++
				if !r.volume.MatchString(sql) {
					t.Errorf("ADR-0008 violated: %s %s charts %s with no volume beside it:\n  %s\n"+
						"  Select %s too. A rate without the volume it is measured over rewards\n"+
						"  timidity: proposing nothing is never rejected.", rel, q.where, r.rate, q.sql, r.name)
				}
			}
		}
	}
	// The acceptance rate is on the Admin API dashboard and the rejection rate on the collector's.
	// If either disappears from every panel, this test has stopped checking what it was written for.
	for _, rate := range []string{"acceptance_rate", "rejection_rate"} {
		if checked[rate] == 0 {
			t.Errorf("no panel charting %s was found to check; this test would pass vacuously", rate)
		}
	}
}

// The two dashboard tests above are only as good as dashboardQueries. This pins the places it must
// reach: a panel inside a collapsed row, both shapes of a query variable, and an annotation. A
// custom variable's query is a list of values, not SQL, and must not be read as SQL.
func TestDashboardQueriesReachEveryQuery(t *testing.T) {
	doc := []byte(`{
	  "title": "fixture",
	  "panels": [
	    {"type": "timeseries", "title": "top", "targets": [{"refId": "A", "rawSql": "SELECT 1 FROM gold_a"}]},
	    {"type": "row", "title": "collapsed", "collapsed": true, "panels": [
	      {"type": "table", "title": "nested", "targets": [{"refId": "A", "rawSql": "SELECT pseudonym FROM silver_b"}]}
	    ]}
	  ],
	  "templating": {"list": [
	    {"type": "query", "name": "as_string", "query": "SELECT DISTINCT x FROM bronze_c"},
	    {"type": "query", "name": "as_object", "query": {"rawSql": "SELECT DISTINCT x FROM silver_d"}},
	    {"type": "query", "name": "unreadable", "query": {}},
	    {"type": "custom", "name": "values", "query": "a,b,c"}
	  ]},
	  "annotations": {"list": [{"name": "marks", "target": {"rawSql": "SELECT t FROM silver_e"}}]}
	}`)
	qs, err := dashboardQueries(doc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, q := range qs {
		got[q.where] = q.sql
	}
	want := map[string]string{
		`panel "top"`:           "SELECT 1 FROM gold_a",
		`panel "nested"`:        "SELECT pseudonym FROM silver_b",
		`variable "as_string"`:  "SELECT DISTINCT x FROM bronze_c",
		`variable "as_object"`:  "SELECT DISTINCT x FROM silver_d",
		`variable "unreadable"`: "",
		`annotation "marks"`:    "SELECT t FROM silver_e",
	}
	if len(qs) != len(want) || !maps.Equal(got, want) {
		t.Errorf("dashboardQueries found:\n  %v\nwant:\n  %v", got, want)
	}
}

func jsonFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every ClickHouse overlay under deploy/compose/clickhouse/ must be mounted by docker-compose.yml.
//
// This is another test that exists because of a real failure. config.d is mounted one file at a
// time rather than as a directory, because a directory bind would shadow the image's own
// docker_related_config.xml and leave the server unreachable. The consequence is that adding an
// overlay file is a two-step change, and doing only the first step fails in the worst possible way:
// the server starts cleanly, logs nothing unusual, reports itself healthy, and runs on stock
// defaults. The file is right there in the repo, so every later reader assumes it is in force.
//
// That is precisely how the idle-wakeup overlay was first "applied" -- written, container
// restarted, and silently ignored, discovered only because system.server_settings still showed the
// defaults it was supposed to have changed. A config that is present but not loaded is worse than
// one that is absent, because absence is visible.
func TestDeployOverlaysAreAllMounted(t *testing.T) {
	root := repoRoot(t)
	composePath := filepath.Join(root, "deploy", "compose", "docker-compose.yml")
	compose, err := os.ReadFile(composePath)
	if err != nil {
		t.Skip("no compose stack yet:", err)
	}
	text := string(compose)

	var checked int
	for _, dir := range []string{"config.d", "users.d"} {
		hostDir := filepath.Join(root, "deploy", "compose", "clickhouse", dir)
		entries, err := os.ReadDir(hostDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
				continue
			}
			checked++
			// The exact string the compose file must contain: the source side of the bind.
			want := "./clickhouse/" + dir + "/" + e.Name() + ":"
			if !strings.Contains(text, want) {
				t.Errorf("deploy/compose/clickhouse/%s/%s is not mounted by docker-compose.yml.\n"+
					"  ClickHouse will start without it and run on defaults, reporting no error.\n"+
					"  Add to the clickhouse service's volumes:\n"+
					"    - ./clickhouse/%s/%s:/etc/clickhouse-server/%s/%s:ro",
					dir, e.Name(), dir, e.Name(), dir, e.Name())
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no ClickHouse overlay files to check; this test is not testing anything")
	}
	t.Logf("checked %d ClickHouse overlay files against the compose mounts", checked)
}

// INV-7 at the edge proxy that serves the collector over the network (docker-compose.network.yml).
//
// Caddy's defaults suit a public website: it fetches certificates from an ACME CA, staples OCSP
// responses fetched from the CA, and listens with an admin API. Here the certificate is obtained
// by hand and only served, so all three stay off. The proxy routes to the collector and nothing
// else: ClickHouse on the network is every pseudonymous row one query away. It sets no request-body
// limit, because a limit below the collector's own loses large hook payloads without a trace. And
// the image is pinned to an exact version, as the collector's is.
func TestINV7_EdgeProxyMakesNoOutboundCalls(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "compose", "edge", "Caddyfile"))
	if err != nil {
		t.Skip("no edge proxy yet:", err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	has := func(want string) bool {
		for _, l := range lines {
			if l == want {
				return true
			}
		}
		return false
	}

	for _, want := range []string{"admin off", "auto_https off", "ocsp_stapling off"} {
		if !has(want) {
			t.Errorf("INV-7: the Caddyfile's global options lack %q. Without it Caddy calls out "+
				"or listens where nothing here needs it to.", want)
		}
	}

	allowed := map[string]bool{"collector:4318": true, "collector:8088": true}
	var proxies, certs int
	for _, l := range lines {
		f := strings.Fields(l)
		switch f[0] {
		case "reverse_proxy":
			proxies++
			for _, up := range f[1:] {
				if !allowed[up] {
					t.Errorf("the edge proxy routes to %q. It serves the collector's two ports only; "+
						"ClickHouse or Grafana on the network needs its own decision (risks.md #15).", up)
				}
			}
		case "tls":
			certs++
			if len(f) != 3 || !strings.HasSuffix(f[1], ".pem") || !strings.HasSuffix(f[2], ".pem") {
				t.Errorf("INV-7: %q. Each site serves a mounted certificate and key; any other form "+
					"of the tls directive has Caddy obtain one itself.", l)
			}
		case "request_body", "max_size":
			t.Errorf("the edge proxy sets a request-body limit (%q). The collector enforces its own; "+
				"a lower one here refuses large hook payloads with no log line.", l)
		}
	}
	if proxies == 0 || certs == 0 {
		t.Fatalf("found %d reverse_proxy and %d tls lines; the parser is wrong or the Caddyfile is",
			proxies, certs)
	}

	compose, err := os.ReadFile(filepath.Join(root, "deploy", "compose", "docker-compose.network.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s*image:\s*caddy:\d+\.\d+\.\d+\s*$`).Match(compose) {
		t.Error("docker-compose.network.yml must pin the caddy image to an exact version, as the " +
			"collector's is. A floating tag changes the proxy under a running deployment.")
	}
}

// The preview stack (scripts/preview.sh) holds a copy of a live stack's rows, so it is held to the
// reference stack's rules, and kept apart from the stack it copies:
//
//   - every port it publishes is on loopback, as docker-compose.yml's are (INV-7);
//   - its ports replace the reference stack's with !override. Merged instead, it would also claim
//     3001 and 8124, which the live stack holds;
//   - it builds cardo under its own tag, so a preview never replaces the image the live stack runs;
//   - it never starts a collector, so nothing can send to it by mistake;
//   - the script sends the live stack nothing but read-only queries.
func TestPreviewStackStaysLocalAndApart(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "compose", "docker-compose.preview.yml"))
	if err != nil {
		t.Skip("no preview stack yet:", err)
	}
	compose := strings.ReplaceAll(string(raw), "\r\n", "\n")

	mappings := regexp.MustCompile(`(?m)^\s*-\s*"([^"]*:\d+)"\s*$`).FindAllStringSubmatch(compose, -1)
	if len(mappings) < 2 {
		t.Fatalf("found %d port mappings in docker-compose.preview.yml; expected Grafana's and "+
			"ClickHouse's. The parser is wrong or the file is.", len(mappings))
	}
	for _, m := range mappings {
		if !strings.HasPrefix(m[1], "127.0.0.1:") {
			t.Errorf("INV-7: the preview publishes %q. It holds a copy of real rows; bind it to "+
				"127.0.0.1 as docker-compose.yml does.", m[1])
		}
	}
	all := regexp.MustCompile(`(?m)^\s*ports:`).FindAllString(compose, -1)
	overridden := regexp.MustCompile(`(?m)^\s*ports:\s*!override\s*$`).FindAllString(compose, -1)
	if len(all) != len(overridden) {
		t.Errorf("%d of the preview's %d ports: keys lack !override. Without it compose merges "+
			"the reference stack's ports in, and the preview claims the live stack's.",
			len(all)-len(overridden), len(all))
	}
	if !regexp.MustCompile(`(?m)^\s*image:\s*cardo-preview:dev\s*$`).MatchString(compose) {
		t.Error("the preview's migrate service must build cardo as cardo-preview:dev. With the " +
			"reference stack's tag, building a branch replaces the image the live stack runs.")
	}
	if !regexp.MustCompile(`(?m)^  collector:\n(\s+#.*\n)*\s+profiles:`).MatchString(compose) {
		t.Error("the preview must never start a collector: give it a profile nobody activates.")
	}

	script, err := os.ReadFile(filepath.Join(root, "scripts", "preview.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var uses int
	for _, l := range strings.Split(string(script), "\n") {
		if strings.Contains(l, `"$LIVE"`) {
			uses++
			if !strings.Contains(l, "--readonly 1") || strings.Contains(l, "exec -i") {
				t.Errorf("scripts/preview.sh reaches the live stack without --readonly 1, or with "+
					"stdin: %q. Every query to it goes through live_ch.", strings.TrimSpace(l))
			}
		}
	}
	if uses != 1 {
		t.Errorf("scripts/preview.sh uses the live container on %d lines; only live_ch should, once", uses)
	}
}
