package test

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

	var panels int
	for _, path := range jsonFiles(t, dir) {
		rel, _ := filepath.Rel(repoRoot(t), path)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var dash struct {
			Panels []struct {
				Title   string `json:"title"`
				Type    string `json:"type"`
				Targets []struct {
					RawSQL string `json:"rawSql"`
				} `json:"targets"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(b, &dash); err != nil {
			t.Errorf("%s is not valid JSON: %v", rel, err)
			continue
		}

		for _, p := range dash.Panels {
			for _, tgt := range p.Targets {
				sql := strings.ToLower(tgt.RawSQL)
				if sql == "" {
					continue
				}
				panels++

				if strings.Contains(sql, "pseudonym") {
					t.Errorf("INV-3 violated: %s panel %q names a pseudonym:\n  %s\n"+
						"  Dashboards are cohort-only. A per-person row on a dashboard is a\n"+
						"  ranking a manager can sort, which is the thing this project promises\n"+
						"  not to build.", rel, p.Title, tgt.RawSQL)
				}
				for _, layer := range []string{"bronze_", "silver_"} {
					if strings.Contains(sql, layer) {
						t.Errorf("INV-3 violated: %s panel %q reads %s directly.\n"+
							"  Dashboards read gold views. Bronze and silver carry a pseudonym\n"+
							"  column, so reading them is one SELECT away from a leaderboard.",
							rel, p.Title, strings.TrimSuffix(layer, "_"))
					}
				}
				if !strings.Contains(sql, "gold_") {
					t.Errorf("INV-3: %s panel %q reads no gold view:\n  %s",
						rel, p.Title, tgt.RawSQL)
				}
			}
		}
	}
	if panels == 0 {
		t.Fatal("no dashboard queries were checked; this test would pass vacuously")
	}
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
		var dash struct {
			Panels []struct {
				Title   string `json:"title"`
				Targets []struct {
					RawSQL string `json:"rawSql"`
				} `json:"targets"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(b, &dash); err != nil {
			continue
		}
		for _, p := range dash.Panels {
			for _, tgt := range p.Targets {
				sql := strings.ToLower(tgt.RawSQL)
				for _, r := range rateVolumes {
					if !strings.Contains(sql, r.rate) {
						continue
					}
					checked[r.rate]++
					if !r.volume.MatchString(sql) {
						t.Errorf("ADR-0008 violated: %s panel %q charts %s with no volume beside it:\n  %s\n"+
							"  Select %s too. A rate without the volume it is measured over rewards\n"+
							"  timidity: proposing nothing is never rejected.", rel, p.Title, r.rate, tgt.RawSQL, r.name)
					}
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

// ClickHouse is healthy only when other containers can reach it.
//
// Another real failure: on a fresh volume the image's entrypoint runs a temporary server bound to
// 127.0.0.1 to create the user and database, then stops it and starts the real one, a few seconds
// later. A healthcheck on localhost passed against the temporary server, compose reported
// "healthy", and migrate, which waits for exactly that, was refused at clickhouse:8123. Migrate
// does not restart, and the collector waits for migrate, so a first `docker compose up -d` could
// end with no collector. Probing by the service name goes through the network address, where the
// temporary server does not listen.
func TestDeployClickHouseHealthyMeansReachable(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "compose", "docker-compose.yml"))
	if err != nil {
		t.Skip("no compose stack yet:", err)
	}
	m := regexp.MustCompile(`(?m)^  clickhouse:\n(?:    .*\n|\s*\n)*?    healthcheck:\n(?:      #.*\n)*      test:(.*)`).
		FindStringSubmatch(strings.ReplaceAll(string(raw), "\r\n", "\n"))
	if m == nil {
		t.Fatal("found no healthcheck test on the clickhouse service in docker-compose.yml. " +
			"The parser is wrong or the file is.")
	}
	if !strings.Contains(m[1], `"--host", "clickhouse"`) {
		t.Errorf("the clickhouse healthcheck is %s. It must connect by the service name "+
			`("--host", "clickhouse"): on localhost it passes against the entrypoint's temporary `+
			"server, and migrate is refused.", strings.TrimSpace(m[1]))
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
//   - its Grafana has its own session cookie, so it does not sign you out of the live one;
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
	if m := regexp.MustCompile(`(?m)^\s*GF_AUTH_LOGIN_COOKIE_NAME:\s*(\S+)\s*$`).FindStringSubmatch(compose); m == nil || m[1] == "grafana_session" {
		t.Error("the preview's Grafana needs its own GF_AUTH_LOGIN_COOKIE_NAME. Browsers keep " +
			"cookies by host, not port, so with the default, signing in to it signs you out of the " +
			"live Grafana on the same 127.0.0.1, within seconds.")
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
