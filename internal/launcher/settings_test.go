package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The launcher generates the bundle instead of reading the repository's copy, so that an installed
// binary does not depend on a checkout. This test keeps the generated document identical to the one
// the project ships and documents.
func TestSettingsBundleMatchesTheShippedFile(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("..", "..", "deploy", "managed-settings", "local-evaluation.json"))
	if err != nil {
		t.Fatalf("read shipped bundle: %v", err)
	}
	generated, err := SettingsBundle()
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if err := json.Unmarshal(shipped, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(generated, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("the generated settings bundle has drifted from deploy/managed-settings/local-evaluation.json")
	}
}

func TestInstallSettingsWritesTheTwelveHooksAndKeepsForeignKeys(t *testing.T) {
	project := t.TempDir()
	claude := filepath.Join(project, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"permissions":{"allow":["Bash(go test:*)"]},"env":{"SOMETHING_ELSE":"1"}}`
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle, err := SettingsBundle()
	if err != nil {
		t.Fatal(err)
	}
	path, err := InstallSettings(project, bundle)
	if err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["permissions"]; !ok {
		t.Error("permissions from the existing file were dropped")
	}
	env, _ := got["env"].(map[string]any)
	if _, ok := env["SOMETHING_ELSE"]; ok {
		t.Error("an unrelated env key survived the merge; env must be replaced wholesale")
	}
	if env["OTEL_LOG_USER_PROMPTS"] != "0" {
		t.Error("content logging was not disabled in the installed file")
	}
	hooks, _ := got["hooks"].(map[string]any)
	if len(hooks) != 12 {
		t.Errorf("hooks = %d, want 12", len(hooks))
	}
	if _, ok := hooks["SessionStart"]; ok {
		t.Error("SessionStart must not be hooked: Claude Code does not run HTTP hooks on it")
	}
}

func TestConnectNeedsAProjectAndThenInstalls(t *testing.T) {
	opts := Options{Dir: t.TempDir()}

	res, err := RunStep(context.Background(), opts, "connect")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("connect succeeded without a project directory")
	}
	if res.Fix == "" {
		t.Error("the failure does not say what to do")
	}

	opts.Project = t.TempDir()
	res, err = RunStep(context.Background(), opts, "connect")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("connect failed: %s", res.Value)
	}
	if _, err := os.Stat(filepath.Join(opts.Project, ".claude", "settings.json")); err != nil {
		t.Fatalf("settings file not installed: %v", err)
	}
}

func TestConnectRefusesAMissingDirectory(t *testing.T) {
	opts := Options{Dir: t.TempDir(), Project: filepath.Join(t.TempDir(), "does-not-exist")}
	res, err := RunStep(context.Background(), opts, "connect")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("connect accepted a directory that does not exist")
	}
}
