package test

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Managed-settings bundle tests.
//
// The bundle is the entire client side of Cardo: there is no agent, and this file is what an
// engineer reads to find out what is collected about them (ADR-0011). These tests hold it to what
// the README and privacy.md promise.

// mandatoryHookEvents is the hook pack. INV-4: the mandatory event set is published and short,
// and adding to it requires an ADR. This list is that set; ADR-0005 is where it was decided.
var mandatoryHookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "UserPromptExpansion", "PermissionRequest",
	"PermissionDenied", "PreCompact", "PostCompact", "InstructionsLoaded", "SubagentStart",
	"SubagentStop", "PreModelSwitch", "ConfigChange",
}

// contentFlags are the Claude Code settings that would put content into telemetry. Each must be
// present and "0" in every bundle: present, so an engineer reading the file sees the decision
// rather than having to know the default.
var contentFlags = []string{
	"OTEL_LOG_USER_PROMPTS", "OTEL_LOG_ASSISTANT_RESPONSES", "OTEL_LOG_TOOL_DETAILS",
	"OTEL_LOG_TOOL_CONTENT", "OTEL_LOG_RAW_API_BODIES", "OTEL_LOG_MANAGED_SETTINGS",
}

type bundleFile struct {
	path string
	top  map[string]json.RawMessage
	env  map[string]string
	// hooks: event -> matcher groups -> handlers, each handler kept as a raw map so that a field
	// this test does not know about is visible rather than silently ignored.
	hooks map[string][]struct {
		Matcher string           `json:"matcher"`
		Hooks   []map[string]any `json:"hooks"`
	}
}

func bundles(t *testing.T) []bundleFile {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "deploy", "managed-settings")
	var out []bundleFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot(t), path)
		bf := bundleFile{path: filepath.ToSlash(rel)}
		if err := json.Unmarshal(b, &bf.top); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		json.Unmarshal(bf.top["env"], &bf.env)
		json.Unmarshal(bf.top["hooks"], &bf.hooks)
		out = append(out, bf)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no bundles found in deploy/managed-settings; these tests would pass vacuously")
	}
	return out
}

// The bundle configures telemetry and nothing else.
//
// Managed settings can do a great deal more: lock permissions, disable every personal hook
// (allowManagedHooksOnly), restrict HTTP hooks to an allowlist (allowedHttpHookUrls), pin versions.
// Each of those changes how Claude Code behaves for the engineer, and "installing Cardo broke my
// setup" is exactly the sentence this project cannot afford. Organizations that want those
// controls set them in their own policy, where they are the organization's decision.
func TestBundleConfiguresTelemetryAndNothingElse(t *testing.T) {
	for _, b := range bundles(t) {
		for k := range b.top {
			if k != "env" && k != "hooks" {
				t.Errorf("%s sets %q. The bundle configures telemetry and nothing else; policy "+
					"belongs in the organization's own managed settings.", b.path, k)
			}
		}
		for k := range b.env {
			if k != "CLAUDE_CODE_ENABLE_TELEMETRY" && !strings.HasPrefix(k, "OTEL_") {
				t.Errorf("%s forces env %q, which is not telemetry configuration", b.path, k)
			}
		}
	}
}

// INV-5 — every content flag is present and off. Repository identity is attached: the collector
// classifies it and drops the URL before storage (ADR-0035, pinned in test/collector_test.go).
func TestINV5_BundlePinsEveryContentFlagOff(t *testing.T) {
	for _, b := range bundles(t) {
		for _, f := range contentFlags {
			v, ok := b.env[f]
			switch {
			case !ok:
				t.Errorf("INV-5: %s does not set %s. Set it to \"0\" explicitly, so the decision is "+
					"on the page for the engineer reading it.", b.path, f)
			case v != "0":
				t.Errorf("INV-5: %s sets %s=%q. Content never enters tiers 0 or 1.", b.path, f, v)
			}
		}
		if b.env["OTEL_METRICS_INCLUDE_REPOSITORY"] != "true" {
			t.Errorf("%s must set OTEL_METRICS_INCLUDE_REPOSITORY=true: which repository a session "+
				"worked in is classified at the collector (ADR-0035)", b.path)
		}
		if b.env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
			t.Errorf("%s does not enable telemetry", b.path)
		}
	}
}

// A count taken from a metric, such as lines of code per session, is the sum of its points. That
// is right only when each point is the change since the previous export: a cumulative series
// repeats its running total every minute, and summing it counts the same lines again on every
// export. Delta is Claude Code's documented default; the bundle writes it out so the decision is
// on the page and a change of default cannot inflate every count.
func TestBundlePinsDeltaTemporality(t *testing.T) {
	for _, b := range bundles(t) {
		if v := b.env["OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE"]; v != "delta" {
			t.Errorf("%s must set OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=delta (found %q): "+
				"a count is the sum of a metric's points", b.path, v)
		}
	}
}

// INV-4 — the bundle hooks exactly the published mandatory events.
func TestINV4_BundleHooksExactlyTheMandatoryEvents(t *testing.T) {
	want := append([]string(nil), mandatoryHookEvents...)
	sort.Strings(want)
	for _, b := range bundles(t) {
		var got []string
		for e := range b.hooks {
			got = append(got, e)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("INV-4: %s hooks %v,\n  but the published mandatory set is %v.\n"+
				"  Adding an event to the mandatory tier needs an ADR first.", b.path, got, want)
		}
	}
}

// Every hook is async HTTP to the same collector the OTel data goes to. Async is the property
// that matters most: a synchronous hook blocks the engineer's session on the collector, and on
// PreModelSwitch a timeout blocks the switch outright. Cardo must never be in the way.
func TestBundleHooksNeverBlockASession(t *testing.T) {
	for _, b := range bundles(t) {
		otlp, err := url.Parse(b.env["OTEL_EXPORTER_OTLP_ENDPOINT"])
		if err != nil || otlp.Hostname() == "" {
			t.Errorf("%s: OTEL_EXPORTER_OTLP_ENDPOINT is not a URL: %q", b.path, b.env["OTEL_EXPORTER_OTLP_ENDPOINT"])
			continue
		}
		for event, groups := range b.hooks {
			for _, g := range groups {
				if g.Matcher != "" {
					t.Errorf("%s %s: matcher %q. The pack observes every occurrence; a matcher would "+
						"make the data silently partial", b.path, event, g.Matcher)
				}
				for _, h := range g.Hooks {
					for k := range h {
						if k != "type" && k != "url" && k != "async" {
							// headers / allowedEnvVars would interpolate the engineer's environment
							// into a request; timeout and statusMessage are for blocking hooks.
							t.Errorf("%s %s: handler sets %q; a Cardo hook is type, url and async only",
								b.path, event, k)
						}
					}
					if h["type"] != "http" {
						t.Errorf("%s %s: handler type %v, want http. There is no agent (ADR-0011).",
							b.path, event, h["type"])
					}
					if h["async"] != true {
						t.Errorf("%s %s: handler is not async. A synchronous hook blocks the "+
							"engineer's session on the collector.", b.path, event)
					}
					u, err := url.Parse(h["url"].(string))
					if err != nil || u.Hostname() != otlp.Hostname() || u.Path != "/v1/hooks" {
						t.Errorf("%s %s: hook URL %v is not the collector's /v1/hooks on %s",
							b.path, event, h["url"], otlp.Hostname())
					}
				}
			}
		}
	}
}

// The local-evaluation bundle must point at the ports the reference stack actually publishes, or
// someone following the README gets a Claude Code that silently sends nothing anywhere.
func TestLocalBundleMatchesTheReferenceStack(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "compose", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bundles(t) {
		if !strings.HasSuffix(b.path, "local-evaluation.json") {
			continue
		}
		otlp, _ := url.Parse(b.env["OTEL_EXPORTER_OTLP_ENDPOINT"])
		hookURL, _ := url.Parse(b.hooks["SessionStart"][0].Hooks[0]["url"].(string))
		for _, want := range []string{
			`"127.0.0.1:` + otlp.Port() + `:4318"`,
			`"127.0.0.1:` + hookURL.Port() + `:8088"`,
		} {
			if !strings.Contains(string(compose), want) {
				t.Errorf("local-evaluation.json expects the stack to publish %s, and "+
					"docker-compose.yml does not", want)
			}
		}
	}
}

// INV-6 — nothing Cardo ships pins a maximum Claude Code version.
func TestINV6_NothingShippedPinsAMaximumVersion(t *testing.T) {
	root := repoRoot(t)
	for _, dir := range []string{"deploy", "dashboards"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, _ := os.ReadFile(path)
			if strings.Contains(string(b), "requiredMaximumVersion") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("INV-6: %s mentions requiredMaximumVersion. Never gate an organization's ability "+
					"to upgrade Claude Code.", rel)
			}
			return nil
		})
	}
}
