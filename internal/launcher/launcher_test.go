package launcher

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- secrets and .env -------------------------------------------------------

func TestGenerateSecretsAreHexOfTheRightLength(t *testing.T) {
	var seed []byte
	for i := 0; i < 4096; i++ {
		seed = append(seed, byte(i%251))
	}
	s, err := GenerateSecrets(bytes.NewReader(seed))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"salt":    s.Salt,
		"ch":      s.ClickHousePassword,
		"grafana": s.GrafanaPassword,
	} {
		if len(got) < 32 {
			t.Errorf("%s = %q, want at least 32 characters", name, got)
		}
		for _, r := range got {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Fatalf("%s = %q, want hex only", name, got)
			}
		}
	}
	if s.Salt == s.ClickHousePassword || s.ClickHousePassword == s.GrafanaPassword {
		t.Error("generated secrets repeat, want three distinct values")
	}
}

func TestValidateEnv(t *testing.T) {
	valid := map[string]string{
		"CLICKHOUSE_PASSWORD": "9f3c9f3c",
		"GRAFANA_PASSWORD":    "1a2b1a2b",
		"CARDO_SALT":          strings.Repeat("ab", 32),
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]string)
		want   string // substring of the first problem; empty means valid
	}{
		{"valid", func(m map[string]string) {}, ""},
		{"no salt", func(m map[string]string) { delete(m, "CARDO_SALT") }, "CARDO_SALT"},
		{"short salt", func(m map[string]string) { m["CARDO_SALT"] = "abcd" }, "32"},
		{"non-hex salt", func(m map[string]string) { m["CARDO_SALT"] = strings.Repeat("zz", 32) }, "hex"},
		{"placeholder password", func(m map[string]string) { m["CLICKHOUSE_PASSWORD"] = "change-me" }, "CLICKHOUSE_PASSWORD"},
		{"empty grafana password", func(m map[string]string) { m["GRAFANA_PASSWORD"] = "" }, "GRAFANA_PASSWORD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]string{}
			for k, v := range valid {
				m[k] = v
			}
			tc.mutate(m)
			got := ValidateEnv(m)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("problems = %v, want none", got)
				}
				return
			}
			if len(got) == 0 || !strings.Contains(strings.Join(got, "; "), tc.want) {
				t.Fatalf("problems = %v, want one mentioning %q", got, tc.want)
			}
		})
	}
}

func TestWriteDotEnvIsPrivateAndKeepsUnrelatedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	s := Secrets{Salt: strings.Repeat("ab", 32), ClickHousePassword: "cafe", GrafanaPassword: "beef"}
	existing := map[string]string{"CARDO_ORG_ARTIFACTS": "^acme-", "CARDO_MIN_GROUP_SIZE": "5"}

	if err := WriteDotEnv(path, existing, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if ok, why := restrictedToOwner(path); !ok {
		t.Errorf("the file is not readable only by its owner: %s", why)
	}

	got, err := ReadDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["CARDO_SALT"] != s.Salt || got["GRAFANA_PASSWORD"] != s.GrafanaPassword {
		t.Errorf("round trip lost a secret: %v", got)
	}
	if got["CARDO_ORG_ARTIFACTS"] != "^acme-" {
		t.Errorf("CARDO_ORG_ARTIFACTS = %q, want the existing value kept", got["CARDO_ORG_ARTIFACTS"])
	}
}

func TestWriteDotEnvDoesNotOverwriteValidSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	first := Secrets{Salt: strings.Repeat("11", 32), ClickHousePassword: "aaaa", GrafanaPassword: "bbbb"}
	if err := WriteDotEnv(path, nil, first); err != nil {
		t.Fatal(err)
	}
	second := Secrets{Salt: strings.Repeat("22", 32), ClickHousePassword: "cccc", GrafanaPassword: "dddd"}
	if err := WriteDotEnv(path, nil, second); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadDotEnv(path)
	if got["CARDO_SALT"] != first.Salt {
		t.Errorf("salt = %q, want the first value kept", got["CARDO_SALT"])
	}
}

// --- ports ------------------------------------------------------------------

func TestPortsFreeNamesTheBusyOne(t *testing.T) {
	busy, err := listenLocal()
	if err != nil {
		t.Skipf("cannot open a local listener: %v", err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	got, err := BusyPorts("127.0.0.1", []int{port})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != port {
		t.Fatalf("busy = %v, want [%d]", got, port)
	}

	free, err := freeLocalPort()
	if err != nil {
		t.Skipf("cannot find a free port: %v", err)
	}
	got, err = BusyPorts("127.0.0.1", []int{free})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("busy = %v for a free port, want none", got)
	}
}

// --- state ------------------------------------------------------------------

func TestStateSurvivesACorruptFileAndMarksSteps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := LoadState(path)
	if len(st.Steps) != 0 {
		t.Fatalf("steps = %v, want empty for a corrupt file", st.Steps)
	}
	st.Mark("docker", "v27.3", true)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	again := LoadState(path)
	if got := again.Steps["docker"].Value; got != "v27.3" {
		t.Errorf("value = %q, want v27.3", got)
	}
	if !again.Done("docker") {
		t.Error("docker should be recorded as done")
	}
}

// --- orchestration ----------------------------------------------------------

type fakeDeps struct {
	calls   []string
	last    map[string][]string
	execErr error
	busy    []int
	httpErr error
	started bool
}

// lastArgs is the argument vector of the most recent call to name.
func (f *fakeDeps) lastArgs(name string) []string { return f.last[name] }

func (f *fakeDeps) deps() Deps {
	return Deps{
		Exec: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			f.calls = append(f.calls, name+" "+strings.Join(args, " "))
			if f.last == nil {
				f.last = map[string][]string{}
			}
			f.last[name] = args
			if name == "docker" && len(args) > 2 && args[0] == "compose" && args[1] == "version" {
				return "v5.2.0", nil
			}
			if name == "docker" && len(args) > 1 && args[0] == "info" {
				if f.execErr != nil {
					return "", f.execErr
				}
				return "27.3.1", nil
			}
			if name == "docker" && len(args) > 1 && args[0] == "compose" && args[1] == "up" && f.execErr == nil {
				f.started = true
			}
			return "", nil
		},
		BusyPorts: func(host string, ports []int) ([]int, error) {
			if len(f.busy) > 0 {
				return f.busy, nil
			}
			if f.started {
				return PublishPorts, nil
			}
			return nil, nil
		},
		WaitHTTP: func(ctx context.Context, url string, timeout time.Duration) error {
			if f.httpErr != nil {
				return f.httpErr
			}
			if !f.started {
				return errors.New("nothing is listening yet")
			}
			return nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) },
	}
}

func TestUpWritesSecretsOnceAndStartsTheStack(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir)
	f := &fakeDeps{}
	var out bytes.Buffer
	opts := Options{Dir: dir, Out: &out, Deps: f.deps()}

	if err := Up(context.Background(), opts); err != nil {
		t.Fatalf("first up: %v", err)
	}
	env, err := ReadDotEnv(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	salt := env["CARDO_SALT"]
	if len(salt) < 32 {
		t.Fatalf("salt = %q, want a generated salt", salt)
	}
	if !hasCall(f.calls, "docker compose up -d --wait") {
		t.Errorf("calls = %v, want compose up", f.calls)
	}
	if !strings.Contains(out.String(), "ГОТОВО") && !strings.Contains(out.String(), "OK") {
		t.Errorf("output does not report a finished board:\n%s", out.String())
	}

	// A second run must not rotate the salt.
	f.calls = nil
	if err := Up(context.Background(), opts); err != nil {
		t.Fatalf("second up: %v", err)
	}
	again, _ := ReadDotEnv(filepath.Join(dir, ".env"))
	if again["CARDO_SALT"] != salt {
		t.Error("second run rotated the salt; up must be idempotent")
	}
}

func TestUpStopsAtDockerAndSaysWhy(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir)
	f := &fakeDeps{execErr: errors.New("Cannot connect to the Docker daemon")}
	err := Up(context.Background(), Options{Dir: dir, Out: &bytes.Buffer{}, Deps: f.deps()})
	if err == nil {
		t.Fatal("want an error when Docker is down")
	}
	if !strings.Contains(err.Error(), "Docker") {
		t.Errorf("error = %v, want it to name Docker", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".env")); statErr == nil {
		t.Error(".env was written before the preflight passed")
	}
}

func TestUpRefusesBusyPorts(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir)
	f := &fakeDeps{busy: []int{8124}}
	err := Up(context.Background(), Options{Dir: dir, Out: &bytes.Buffer{}, Deps: f.deps()})
	if err == nil || !strings.Contains(err.Error(), "8124") {
		t.Fatalf("error = %v, want it to name the busy port", err)
	}
}

func TestCheckIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir)
	f := &fakeDeps{}
	res, err := Check(context.Background(), Options{Dir: dir, Deps: f.deps()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("want at least one row")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "compose up") {
			t.Fatalf("check ran %q; it must not start anything", c)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".env")); statErr == nil {
		t.Error("check wrote .env; it must be read-only")
	}
}

// --- helpers ----------------------------------------------------------------

func writeCompose(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// A failure without a cause and a recovery is a dead end for the person looking at the board. Every
// step must be able to explain itself, and the explanation must come from the observed output.
func TestEveryFailureExplainsItself(t *testing.T) {
	f := &fakeDeps{
		execErr: errors.New("Cannot connect to the Docker daemon"),
		httpErr: errors.New("connection refused"),
	}
	opts := Options{Dir: t.TempDir(), Out: &bytes.Buffer{}, Deps: f.deps()}

	for _, id := range []string{"docker", "stack", "clickhouse", "grafana", "collector", "connect"} {
		res, err := RunStep(context.Background(), opts, id)
		if err != nil {
			t.Fatalf("%s returned an error instead of a result: %v", id, err)
		}
		if res.OK {
			t.Fatalf("%s unexpectedly passed", id)
		}
		if res.Cause == "" {
			t.Errorf("%s has no cause", id)
		}
		if res.Fix == "" {
			t.Errorf("%s has no recovery", id)
		}
	}
}

// A port that is already taken must fail the step, not pass with a note in an unread field.
func TestPortsStepFailsWhenSomethingListens(t *testing.T) {
	f := &fakeDeps{busy: []int{8124, 8089}}
	res, err := RunStep(context.Background(), Options{Dir: t.TempDir(), Deps: f.deps()}, "ports")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("the ports step passed while 8124 and 8089 were held")
	}
	if !strings.Contains(res.Cause, "8124") {
		t.Errorf("cause = %q, want it to name a held port", res.Cause)
	}
	if res.Fix == "" {
		t.Error("no recovery for a held port")
	}
}

// A second run on a healthy machine must not fail the ports step: all five ports held by our own
// stack is the installed state.
func TestPortsStepAcceptsAnAlreadyRunningStack(t *testing.T) {
	f := &fakeDeps{busy: []int{8124, 9001, 3001, 4319, 8089}, started: true}
	res, err := RunStep(context.Background(), Options{Dir: t.TempDir(), Deps: f.deps()}, "ports")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("ports failed on an installed stack: %s / %s", res.Cause, res.Fix)
	}
	if !strings.Contains(res.Value, "already running") {
		t.Errorf("value = %q, want it to say the stack is running", res.Value)
	}
}

// All five ports held by something that is not our stack is still a failure.
func TestPortsStepFailsWhenAllPortsAreForeign(t *testing.T) {
	f := &fakeDeps{busy: []int{8124, 9001, 3001, 4319, 8089}, httpErr: errors.New("refused")}
	res, err := RunStep(context.Background(), Options{Dir: t.TempDir(), Deps: f.deps()}, "ports")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("ports passed while a foreign process held all five")
	}
}

// The secrets file must not be readable by other accounts on the machine.
func TestEnvFileIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Dir: dir, Deps: (&fakeDeps{}).deps()}
	if _, err := RunStep(context.Background(), opts, "secrets"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	if ok, why := restrictedToOwner(filepath.Join(dir, ".env")); !ok {
		t.Fatalf("the file is not readable only by its owner: %s", why)
	}
}
