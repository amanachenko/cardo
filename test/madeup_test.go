package test

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/amanachenko/cardo/internal/madeup"
)

// The made-up organization (ADR-0051) runs a collector that takes a hook event's time from a
// header its generator sends. That rule is what lets twelve weeks of invented history be loaded in
// minutes, and it must never reach the shipped collector, where anyone who can reach the
// collector could backdate events (risks.md #19).

func madeUpFile(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{repoRoot(t), "deploy", "compose"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// The shipped collector never reads the time header, never turns headers into attributes, and
// never mentions the made-up organization. Comments included: a commented-out copy is one
// keystroke from live.
func TestMadeUpTimeRuleNeverShips(t *testing.T) {
	cfg := strings.ToLower(collectorConfig(t))
	for _, banned := range []string{"header_attribute_regex", "x-cardo-made-up-time", "made_up", "made-up"} {
		if strings.Contains(cfg, banned) {
			t.Errorf("deploy/collector/config.yaml mentions %q. The made-up organization's time rule "+
				"lives in deploy/compose/made-up/collector-made-up.yml only (ADR-0051): in the shipped "+
				"collector, anyone who can reach it could backdate events.", banned)
		}
	}
}

// The made-up collector file adds the time rule and nothing else. Anything more would make the
// made-up stack differ from the shipped collector in a way its views would quietly depend on.
func TestMadeUpCollectorAddsOnlyTheTimeRule(t *testing.T) {
	overlay := madeUpFile(t, "made-up", "collector-made-up.yml")

	var top []string
	for _, line := range strings.Split(overlay, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, " ") {
			top = append(top, strings.TrimSuffix(line, ":"))
		}
	}
	if !slices.Equal(top, []string{"receivers", "processors", "service"}) {
		t.Errorf("collector-made-up.yml has the top-level sections %v; want receivers, processors "+
			"and service, nothing else", top)
	}

	receivers := sectionBlocks(overlay, "receivers")
	if len(receivers) != 1 || strings.TrimSpace(receivers["webhook_event"]) !=
		`header_attribute_regex: "(?i)^x-cardo-made-up-time$"` {
		t.Errorf("collector-made-up.yml may change only the hook receiver, and only to turn the "+
			"X-Cardo-Made-Up-Time header into an attribute. It has: %v", receivers)
	}

	processors := sectionBlocks(overlay, "processors")
	block, ok := processors["transform/made_up_time"]
	if len(processors) != 1 || !ok {
		t.Fatalf("collector-made-up.yml may add one processor, transform/made_up_time. It has: %v",
			keys(processors))
	}
	if !strings.Contains(block, "error_mode: propagate") {
		t.Error("transform/made_up_time must use error_mode: propagate, as every collector " +
			"processor does: a statement that errors is otherwise skipped without a word.")
	}
	statements := regexp.MustCompile(`(?m)^\s*- '(.*)'$`).FindAllStringSubmatch(block, -1)
	want := []string{
		`set(log.time_unix_nano, Int(log.attributes["header.X-Cardo-Made-Up-Time"][0])) where IsList(log.attributes["header.X-Cardo-Made-Up-Time"])`,
		`delete_key(log.attributes, "header.X-Cardo-Made-Up-Time")`,
	}
	var got []string
	for _, s := range statements {
		got = append(got, s[1])
	}
	if !slices.Equal(got, want) {
		t.Errorf("transform/made_up_time must set the event's time from the header and remove the "+
			"header, and do nothing else.\n got: %q\nwant: %q", got, want)
	}

	shipped := pipelines(collectorConfig(t))["logs/hooks"]["processors"]
	i := slices.Index(shipped, "transform/hooks")
	if i < 0 {
		t.Fatal("the shipped logs/hooks pipeline has no transform/hooks; the parser is wrong or the config is")
	}
	wantPipeline := slices.Insert(slices.Clone(shipped), i+1, "transform/made_up_time")
	madeUp := pipelines(overlay)
	if len(madeUp) != 1 || len(madeUp["logs/hooks"]) != 1 {
		t.Errorf("collector-made-up.yml may change only the logs/hooks pipeline's processors. It has: %v", madeUp)
	}
	if got := madeUp["logs/hooks"]["processors"]; !slices.Equal(got, wantPipeline) {
		t.Errorf("the made-up logs/hooks pipeline must be the shipped one with transform/made_up_time "+
			"right after transform/hooks. A list in the second config file replaces the shipped list, "+
			"so when the shipped pipeline changes, this one must change with it.\n got: %v\nwant: %v",
			got, wantPipeline)
	}
}

// The made-up stack is held to the reference stack's rules, and kept apart from the stacks that
// hold real rows:
//
//   - its compose project is cardo-madeup, whatever directory it is started from;
//   - every port it publishes is on loopback (INV-7), replaces the reference stack's with
//     !override, and is held by no other stack;
//   - it builds cardo under its own tag;
//   - its collector reads the shipped settings first and the made-up file second;
//   - its Grafana has its own session cookie, and files the same dashboards as every other stack
//     under a folder that says they are made up;
//   - the script sends only to the ports this file publishes.
func TestMadeUpStackStaysLocalAndApart(t *testing.T) {
	compose := madeUpFile(t, "docker-compose.made-up.yml")

	if !regexp.MustCompile(`(?m)^name: cardo-madeup$`).MatchString(compose) {
		t.Error("docker-compose.made-up.yml must name its project cardo-madeup, so it never " +
			"shares containers or volumes with another stack.")
	}

	ports := hostPorts(t, compose)
	if len(ports) != 4 {
		t.Fatalf("found %d port mappings in docker-compose.made-up.yml; expected ClickHouse's, "+
			"OTLP's, the hooks' and Grafana's. The parser is wrong or the file is.", len(ports))
	}
	for _, p := range ports {
		if !strings.HasPrefix(p, "127.0.0.1:") {
			t.Errorf("INV-7: the made-up stack publishes %q. Bind it to 127.0.0.1, as "+
				"docker-compose.yml does.", p)
		}
	}
	all := regexp.MustCompile(`(?m)^\s*ports:`).FindAllString(compose, -1)
	overridden := regexp.MustCompile(`(?m)^\s*ports:\s*!override\s*$`).FindAllString(compose, -1)
	if len(all) != len(overridden) {
		t.Errorf("%d of the made-up stack's %d ports: keys lack !override. Without it compose "+
			"merges the reference stack's ports in, and the made-up stack claims the live stack's.",
			len(all)-len(overridden), len(all))
	}
	for _, other := range []string{"docker-compose.yml", "docker-compose.preview.yml"} {
		for _, p := range hostPorts(t, madeUpFile(t, other)) {
			if slices.Contains(ports, p) {
				t.Errorf("the made-up stack publishes %s, which %s also publishes. Two stacks on one "+
					"port means the generator can send invented sessions to the other one.", p, other)
			}
		}
	}

	if !regexp.MustCompile(`(?m)^\s*image:\s*cardo-madeup:dev\s*$`).MatchString(compose) {
		t.Error("the made-up stack's migrate service must build cardo as cardo-madeup:dev. With " +
			"the reference stack's tag, building a branch replaces the image the live stack runs.")
	}
	if !strings.Contains(compose, `- "--config=/etc/otelcol-contrib/config.yaml"`+"\n"+
		`      - "--config=/etc/otelcol-contrib/made-up.yaml"`) ||
		!strings.Contains(compose, "- ./made-up/collector-made-up.yml:/etc/otelcol-contrib/made-up.yaml:ro") {
		t.Error("the made-up collector must read deploy/collector/config.yaml first and " +
			"made-up/collector-made-up.yml second, mounted read-only. In the other order, the " +
			"shipped pipeline would replace the made-up one, and every hook event would land on " +
			"the day it was loaded.")
	}
	if m := regexp.MustCompile(`(?m)^\s*GF_AUTH_LOGIN_COOKIE_NAME:\s*(\S+)\s*$`).FindStringSubmatch(compose); m == nil ||
		m[1] == "grafana_session" || m[1] == "cardo_preview_session" {
		t.Error("the made-up Grafana needs its own GF_AUTH_LOGIN_COOKIE_NAME. Browsers keep " +
			"cookies by host, not port, so a shared name signs you out of the other Grafanas on " +
			"127.0.0.1.")
	}

	// The provider is the reference stack's, but for the folder, so a change to one is a change
	// to both.
	strip := func(s string) []string {
		var out []string
		for _, line := range strings.Split(s, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") &&
				!strings.HasPrefix(trimmed, "folder:") {
				out = append(out, line)
			}
		}
		return out
	}
	provider := madeUpFile(t, "made-up", "grafana", "dashboards.yml")
	if !slices.Equal(strip(provider), strip(madeUpFile(t, "grafana", "provisioning", "dashboards", "cardo.yml"))) {
		t.Error("made-up/grafana/dashboards.yml must be grafana/provisioning/dashboards/cardo.yml " +
			"with only the folder changed. Change both together.")
	}
	if !regexp.MustCompile(`(?m)^\s*folder: "MADE-UP ORGANIZATION - not real data"$`).MatchString(provider) {
		t.Error("the made-up Grafana must file the dashboards under a folder that says they are " +
			"made up: it is in the path at the top of every dashboard, and so in every screenshot.")
	}

	script, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "made-up.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^project=cardo-madeup$`).Match(script) {
		t.Error("scripts/made-up.sh must work on the cardo-madeup project only.")
	}
	published := map[string]bool{}
	for _, p := range ports {
		published[p[strings.Index(p, ":")+1:strings.LastIndex(p, ":")]] = true
	}
	for _, m := range regexp.MustCompile(`http://127\.0\.0\.1:(\d+)`).FindAllStringSubmatch(string(script), -1) {
		if !published[m[1]] {
			t.Errorf("scripts/made-up.sh sends to port %s, which the made-up stack does not publish. "+
				"It must send only to its own stack.", m[1])
		}
	}
}

// The generator (cmd/cardo-madeup) sends only to the made-up stack. Its addresses are fixed in
// internal/madeup, with no flag to change them, and they must be the ports this stack publishes
// for its collector, which no other stack uses. It names the organization's own artifacts and
// repositories with the patterns this stack's collector and views are given, or the views would
// label what it planted differently from what the answer key says.
func TestMadeUpGeneratorSendsOnlyToTheMadeUpStack(t *testing.T) {
	compose := madeUpFile(t, "docker-compose.made-up.yml")
	for _, c := range []struct{ url, mapping string }{
		{madeup.HooksURL, `"127.0.0.1:8090:8088"`},
		{madeup.HealthURL, `"127.0.0.1:8090:8088"`},
		{madeup.OTLPURL, `"127.0.0.1:4320:4318"`},
	} {
		u, err := url.Parse(c.url)
		if err != nil {
			t.Fatal(err)
		}
		host := strings.Trim(c.mapping, `"`)
		host = host[:strings.LastIndex(host, ":")]
		if u.Scheme != "http" || u.Host != host || !strings.Contains(compose, "- "+c.mapping) {
			t.Errorf("the generator sends to %s; it must send only to the made-up collector, which "+
				"docker-compose.made-up.yml publishes as %s", c.url, c.mapping)
		}
	}

	artifacts := regexp.MustCompile(`(?m)^\s*CARDO_ORG_ARTIFACTS:\s*"(.*)"\s*$`).FindAllStringSubmatch(compose, -1)
	if len(artifacts) != 2 {
		t.Fatalf("expected CARDO_ORG_ARTIFACTS on migrate and on the collector; found it %d times", len(artifacts))
	}
	for _, m := range artifacts {
		if got := strings.ReplaceAll(m[1], "$$", "$"); got != madeup.OrgArtifacts {
			t.Errorf("docker-compose.made-up.yml sets CARDO_ORG_ARTIFACTS to %q; the generator names "+
				"the organization's artifacts with %q. Change both together.", got, madeup.OrgArtifacts)
		}
	}
	repos := regexp.MustCompile(`(?m)^\s*CARDO_ORG_REPOS:\s*"(.*)"\s*$`).FindStringSubmatch(compose)
	if repos == nil || repos[1] != madeup.OrgRepos {
		t.Errorf("docker-compose.made-up.yml's CARDO_ORG_REPOS must be the generator's %q", madeup.OrgRepos)
	}

	overlay := madeUpFile(t, "made-up", "collector-made-up.yml")
	if !strings.Contains(overlay, `"(?i)^`+strings.ToLower(madeup.TimeHeader)+`$"`) ||
		!strings.Contains(overlay, `"header.`+madeup.TimeHeader+`"`) {
		t.Errorf("the generator sends each hook event's time in %s, and the made-up collector must "+
			"read that header", madeup.TimeHeader)
	}
}

// hostPorts returns the "host:port:container" mappings a compose file publishes, minus the
// container port: "127.0.0.1:8126:8123" becomes "127.0.0.1:8126:".
func hostPorts(t *testing.T, compose string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\s*-\s*"([^"]*:\d+):\d+"\s*(#.*)?$`).FindAllStringSubmatch(compose, -1) {
		out = append(out, m[1]+":")
	}
	return out
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
