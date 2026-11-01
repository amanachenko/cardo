package launcher

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amanachenko/cardo/internal/notice"
)

// writeEnv writes a settings file with the given values, so a step reads the same file an
// operator's machine has.
func writeEnv(t *testing.T, dir string, values map[string]string) {
	t.Helper()
	var b strings.Builder
	for k, v := range values {
		b.WriteString(k + "=" + v + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// hostOf reads the host out of a URL, so a test can say two addresses are the same machine.
func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%q is not a URL: %v", raw, err)
	}
	return u.Hostname()
}

// The pilot rows are the ones a team needs. They used to be a placeholder that answered "not
// implemented" while the board drew them, so this test names each one and says what it must do.
func TestThePilotRowsAreBuilt(t *testing.T) {
	for _, id := range []string{"gold", "bundle", "notice"} {
		var found bool
		for _, m := range Steps {
			if m.ID == id {
				found = true
				if !m.Pilot {
					t.Errorf("the %s row is a team row and must be marked Pilot", id)
				}
				if m.Planned {
					t.Errorf("the %s row is built, so it must not be marked Planned", id)
				}
			}
		}
		if !found {
			t.Errorf("there is no %s row", id)
		}
	}
}

// The notice is a page an organization hands to its engineers. It must say who holds the salt,
// which settings are in force, and what the data is not used for.
func TestTheNoticeStepWritesThePage(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{
		"CARDO_SALT":          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"CARDO_SALT_VERSION":  "v1",
		"CARDO_ORG_ARTIFACTS": "^acme-",
	})

	res, err := stepNotice(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("the notice step failed: %s %s", res.Value, res.Fix)
	}
	path := filepath.Join(dir, "engineer-notice.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the notice was not written: %v", err)
	}
	page := string(b)
	for _, want := range []string{
		"the operator",        // who holds the salt
		"CARDO_ORG_ARTIFACTS", // which settings are on
		"not used for individual performance decisions",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}
}

// The salt is the key that could reveal who is behind a code name. The page names it and never
// prints it.
func TestTheNoticeStepNeverPrintsTheSalt(t *testing.T) {
	const salt = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{"CARDO_SALT": salt})

	if _, err := stepNotice(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "engineer-notice.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), salt) {
		t.Error("the page prints the salt")
	}
}

// With the holder key empty the page names the operator who runs Cardo and never claims a security
// team: during a pilot the operator creates and holds the salt (ADR-0034), and the launcher writes
// it into the operator's own .env, so nothing it runs can make a security-team line true. The
// step's result asks the operator to fill the key in.
func TestTheNoticeNamesTheOperatorWhenTheHolderIsNotFilledIn(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{"CARDO_SALT": "0123456789abcdef0123456789abcdef"})

	res, err := stepNotice(Options{Dir: dir, Pilot: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("the notice step failed: %s %s", res.Value, res.Fix)
	}
	if !strings.Contains(res.Value, "CARDO_SALT_HOLDER") {
		t.Errorf("the result does not ask the operator to fill in the holder: %q", res.Value)
	}
	b, err := os.ReadFile(filepath.Join(dir, "engineer-notice.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), notice.CustodyOperator) {
		t.Error("the page does not name the operator")
	}
	if strings.Contains(string(b), "security team") {
		t.Error("the page claims a security team holds the salt while the key is empty")
	}
}

// When the operator has recorded a hand-over, the page names that holder.
func TestTheNoticeNamesTheHolderTheOperatorFilledIn(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{
		"CARDO_SALT":        "0123456789abcdef0123456789abcdef",
		"CARDO_SALT_HOLDER": "the security team",
	})

	if _, err := stepNotice(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "engineer-notice.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "held by the security team") {
		t.Error("the page does not name the holder the operator filled in")
	}
}

// A missing settings file is a cause and a fix, not a stack trace.
func TestTheNoticeStepExplainsAMissingSettingsFile(t *testing.T) {
	dir := t.TempDir()
	res, err := stepNotice(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("the notice step passed with no settings file")
	}
	if res.Cause == "" || res.Fix == "" {
		t.Errorf("the failure has no cause or no fix: %+v", res)
	}
}

// The bundle is the whole client side, and the file the launcher writes must be the shape
// test/bundle_test.go holds the shipped files to: env and hooks and nothing else, every content
// flag off, and every hook waiting at most a second.
func TestTheBundleStepWritesTheShapeTheShippedFileHas(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{"CARDO_SALT": "0123456789abcdef0123456789abcdef"})

	res, err := stepBundle(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("the bundle step failed: %s %s", res.Value, res.Fix)
	}
	b, err := os.ReadFile(filepath.Join(dir, "managed-settings.json"))
	if err != nil {
		t.Fatalf("the bundle was not written: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the bundle is not valid JSON: %v", err)
	}
	for k := range doc {
		if k != "env" && k != "hooks" {
			t.Errorf("the bundle sets %q. It configures telemetry and nothing else (ADR-0024).", k)
		}
	}
	env, _ := doc["env"].(map[string]any)
	for _, f := range []string{
		"OTEL_LOG_USER_PROMPTS", "OTEL_LOG_ASSISTANT_RESPONSES", "OTEL_LOG_TOOL_DETAILS",
		"OTEL_LOG_TOOL_CONTENT", "OTEL_LOG_RAW_API_BODIES", "OTEL_LOG_MANAGED_SETTINGS",
	} {
		if env[f] != "0" {
			t.Errorf("the bundle does not set %s=0 (INV-5): %v", f, env[f])
		}
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if len(hooks) != 12 {
		t.Errorf("the bundle hooks %d events, want the published twelve (INV-4)", len(hooks))
	}
	for event, groups := range hooks {
		list, _ := groups.([]any)
		for _, g := range list {
			group, _ := g.(map[string]any)
			handlers, _ := group["hooks"].([]any)
			for _, h := range handlers {
				handler, _ := h.(map[string]any)
				if handler["type"] != "http" {
					t.Errorf("%s: handler type %v, want http", event, handler["type"])
				}
				if timeout, ok := handler["timeout"].(float64); !ok || timeout <= 0 || timeout > 1 {
					t.Errorf("%s: timeout %v, it must be at most one second (ADR-0039)", event, handler["timeout"])
				}
				for k := range handler {
					if k != "type" && k != "url" && k != "timeout" {
						t.Errorf("%s: handler sets %q; a Cardo hook is type, url and timeout only", event, k)
					}
				}
			}
		}
	}
}

// The hook URL must be the collector's /v1/hooks on the same host as the OTLP endpoint, which is
// what test/bundle_test.go holds the shipped files to.
func TestTheBundlePointsBothAtTheSameCollector(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{"CARDO_SALT": "0123456789abcdef0123456789abcdef"})

	if _, err := stepBundle(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "managed-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Env   map[string]string `json:"env"`
		Hooks map[string][]struct {
			Hooks []struct {
				URL string `json:"url"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	otlp := doc.Env["OTEL_EXPORTER_OTLP_ENDPOINT"]
	hook := doc.Hooks["UserPromptSubmit"][0].Hooks[0].URL
	otlpHost := hostOf(t, otlp)
	if hostOf(t, hook) != otlpHost {
		t.Errorf("the hook URL is on %s and the OTLP endpoint on %s; they are the same collector", hostOf(t, hook), otlpHost)
	}
	if !strings.HasSuffix(hook, "/v1/hooks") {
		t.Errorf("the hook URL %q is not the collector's /v1/hooks", hook)
	}
}

// CARDO_HOSTNAME decides the address whenever it is set — a solo stack behind the network overlay
// needs it too. The edge proxy serves TLS, so the endpoint is https.
func TestTheBundlePointsAtTheNetworkAddressWheneverAHostnameIsSet(t *testing.T) {
	for _, pilot := range []bool{false, true} {
		dir := t.TempDir()
		writeEnv(t, dir, map[string]string{
			"CARDO_SALT":     "0123456789abcdef0123456789abcdef",
			"CARDO_HOSTNAME": "collector.internal.example",
		})

		if _, err := stepBundle(Options{Dir: dir, Pilot: pilot}); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "managed-settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Env map[string]string `json:"env"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		endpoint := doc.Env["OTEL_EXPORTER_OTLP_ENDPOINT"]
		if !strings.Contains(endpoint, "collector.internal.example") {
			t.Errorf("pilot=%v: the bundle points at %q, not at the collector's hostname", pilot, endpoint)
		}
		if !strings.HasPrefix(endpoint, "https://") {
			t.Errorf("pilot=%v: the bundle points at %q; the edge proxy serves TLS", pilot, endpoint)
		}
	}
}

// On the team path without CARDO_HOSTNAME the bundle would point every machine at itself: on each
// engineer's laptop 127.0.0.1 is the laptop. That is a failure with a fix, not a loopback bundle
// that silently never arrives.
func TestTheTeamBundleWithoutAHostnameFails(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, map[string]string{"CARDO_SALT": "0123456789abcdef0123456789abcdef"})

	res, err := stepBundle(Options{Dir: dir, Pilot: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("the team bundle without a hostname passed")
	}
	if res.Value != "no network address for the collector" {
		t.Errorf("value = %q, want %q", res.Value, "no network address for the collector")
	}
	if !strings.Contains(res.Fix, "CARDO_HOSTNAME") {
		t.Errorf("the fix does not name the setting: %q", res.Fix)
	}
	if _, err := os.Stat(filepath.Join(dir, "managed-settings.json")); err == nil {
		t.Error("a bundle was written anyway")
	}
}
