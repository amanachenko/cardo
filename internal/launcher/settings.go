package launcher

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

// SettingsBundle builds the Claude Code settings this stack requires: native telemetry pointed at
// the local collector, the twelve mandatory hooks, and every content-logging flag explicitly off
// (INV-5). test/bundle_test.go enforces the same shape for the shipped file; this is the same
// document, generated so the launcher does not depend on a path inside the repository.
func SettingsBundle() ([]byte, error) {
	return SettingsBundleAt("127.0.0.1", "http", "4319", "8089")
}

// SettingsBundleAt builds the same bundle for a collector reached at the given host and ports.
//
// The single-engineer install uses loopback. A pilot that serves a team reaches the collector
// through the edge proxy, on the hostname the certificate is for, and the hook URL carries the
// same host as the OTLP one because test/bundle_test.go holds that they agree.
func SettingsBundleAt(host, scheme, otlpPort, hookPort string) ([]byte, error) {
	env := map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":                      "1",
		"OTEL_METRICS_EXPORTER":                             "otlp",
		"OTEL_LOGS_EXPORTER":                                "otlp",
		"OTEL_EXPORTER_OTLP_PROTOCOL":                       "http/protobuf",
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE": "delta",
		"OTEL_EXPORTER_OTLP_ENDPOINT":                       endpointAt(host, scheme, otlpPort, ""),
		"OTEL_METRICS_INCLUDE_SESSION_ID":                   "true",
		"OTEL_METRICS_INCLUDE_VERSION":                      "true",
		"OTEL_METRICS_INCLUDE_ACCOUNT_UUID":                 "false",
		"OTEL_METRICS_INCLUDE_REPOSITORY":                   "true",
		"OTEL_RESOURCE_ATTRIBUTES":                          "cardo.cohort=unassigned",
		"OTEL_LOG_USER_PROMPTS":                             "0",
		"OTEL_LOG_ASSISTANT_RESPONSES":                      "0",
		"OTEL_LOG_TOOL_DETAILS":                             "0",
		"OTEL_LOG_TOOL_CONTENT":                             "0",
		"OTEL_LOG_RAW_API_BODIES":                           "0",
		"OTEL_LOG_MANAGED_SETTINGS":                         "0",
	}

	hookURL := endpointAt(host, scheme, hookPort, "/v1/hooks")
	hook := func() []map[string]any {
		return []map[string]any{{
			"hooks": []map[string]any{{"type": "http", "url": hookURL, "timeout": 1}},
		}}
	}
	hooks := map[string]any{}
	for _, event := range []string{
		"SessionEnd", "UserPromptSubmit", "UserPromptExpansion", "PermissionRequest",
		"PermissionDenied", "PreCompact", "PostCompact", "InstructionsLoaded",
		"SubagentStart", "SubagentStop", "PostModelSwitch", "ConfigChange",
	} {
		hooks[event] = hook()
	}

	doc := map[string]any{"env": env, "hooks": hooks}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// endpointAt builds one URL for a host and a port. The host may be a name or an address, and
// JoinHostPort brackets an IPv6 one.
func endpointAt(host, scheme, port, path string) string {
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port), Path: path}
	return u.String()
}

// InstallSettings writes the bundle into projectDir/.claude/settings.json. An existing file keeps
// every key the bundle does not own; env and hooks are replaced, because a partially merged hook
// list would silently drop one of the twelve events.
func InstallSettings(projectDir string, bundle []byte) (string, error) {
	if projectDir == "" {
		return "", fmt.Errorf("no project directory selected")
	}
	dir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "settings.json")

	var incoming map[string]any
	if err := json.Unmarshal(bundle, &incoming); err != nil {
		return "", fmt.Errorf("bundle is not valid JSON: %w", err)
	}

	final := incoming
	if existing, err := os.ReadFile(path); err == nil {
		var current map[string]any
		if err := json.Unmarshal(existing, &current); err == nil {
			for k, v := range current {
				if k == "env" || k == "hooks" {
					continue
				}
				if _, taken := incoming[k]; !taken {
					incoming[k] = v
				}
			}
			final = incoming
		}
	}

	b, err := json.MarshalIndent(final, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}
