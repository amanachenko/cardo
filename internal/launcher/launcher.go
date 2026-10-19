// Package launcher brings a Cardo reference stack up on one machine and proves that data arrives.
//
// It exists to close the first item of Step 1 in docs/roadmap.md: a quickstart for one machine and
// `cardo check`. Every step is idempotent — a second run repairs, it does not reinstall — and every
// failure names one cause and one recovery.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Result is one row of the board: a check, its value, and whether it passed.
type Result struct {
	ID    string
	Name  string
	Value string
	OK    bool
	// Cause states what was observed; Fix states what to do about it. Both are shown on failure and
	// never guessed: a cause that does not match the output is worse than no message.
	Cause string
	Fix   string
	// Reveal carries generated credentials shown exactly once, in the run event. It is never stored
	// in the server's state and never appears in /api/state.
	Reveal map[string]string
	// Pending marks a step that cannot pass yet because it waits for real work (the engineer's own
	// session). It is not a failure and must not be explained as one.
	Pending bool
}

// Deps are the side effects, injected so the orchestration can be tested without Docker.
type Deps struct {
	Exec      func(ctx context.Context, dir, name string, args ...string) (string, error)
	BusyPorts func(host string, ports []int) ([]int, error)
	WaitHTTP  func(ctx context.Context, url string, timeout time.Duration) error
	Now       func() time.Time
}

// Options is what a caller can change. The zero value is the solo path on the default directory.
type Options struct {
	Dir  string // directory holding docker-compose.yml and .env; default deploy/compose
	Out  interface{ Write([]byte) (int, error) }
	Wait time.Duration // how long to wait for ClickHouse after compose up; default 5m
	// GOOS overrides the platform used for remedy text and port diagnostics. Empty means runtime.GOOS;
	// tests set it to assert all three platforms from one host.
	GOOS string
	// URLFn overrides how loopback service URLs are built. Tests point it at httptest servers.
	URLFn func(port, path string) string
	// PollTimeout bounds the read-back wait. Empty means 45s; tests shorten it.
	PollTimeout time.Duration
	// Version is what the board prints in Settings; cmd/cardo sets it from the build.
	Version string
	// Token overrides the board's session token. The CLI keeps one per port so a link survives a
	// restart; tests and the embedded board leave it empty.
	Token string
	Deps  Deps
	// Pilot is kept for the board and the notice; the pilot-only rows (leaders' logins, bundle,
	// notice) land with the next change. It changes nothing in this one.
	Pilot  bool
	DryRun bool
	// Project is the directory whose Claude Code settings the connect step installs into.
	Project string
}

// PublishPorts are the host ports the reference stack binds, all on loopback.
var PublishPorts = []int{8124, 9001, 3001, 4319, 8089}

func (o *Options) defaults() {
	if o.Dir == "" {
		o.Dir = filepath.Join("deploy", "compose")
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.Wait == 0 {
		o.Wait = 5 * time.Minute
	}
	d := DefaultDeps()
	if o.Deps.Exec == nil {
		o.Deps.Exec = d.Exec
	}
	if o.Deps.BusyPorts == nil {
		o.Deps.BusyPorts = d.BusyPorts
	}
	if o.Deps.WaitHTTP == nil {
		o.Deps.WaitHTTP = d.WaitHTTP
	}
	if o.Deps.Now == nil {
		o.Deps.Now = d.Now
	}
}

// url builds a loopback URL for a published port, or the test override.
func (o Options) url(port, path string) string {
	if o.URLFn != nil {
		return o.URLFn(port, path)
	}
	return serviceURL(port, path)
}

func (o Options) pollTimeout() time.Duration {
	if o.PollTimeout > 0 {
		return o.PollTimeout
	}
	return 45 * time.Second
}

func (o Options) goos() string {
	if o.GOOS != "" {
		return o.GOOS
	}
	return runtime.GOOS
}

// DefaultDeps are the real implementations.
func DefaultDeps() Deps {
	return Deps{
		Exec: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			return strings.TrimSpace(string(out)), err
		},
		BusyPorts: BusyPorts,
		WaitHTTP:  WaitHTTP,
		Now:       time.Now,
	}
}

// Up runs the launcher: preflight, secrets, stack, then the data checks. It stops at the first step
// it cannot fix and returns an error that names it.
func Up(ctx context.Context, opts Options) error {
	opts.defaults()
	if err := requireComposeFile(opts.Dir); err != nil {
		return err
	}

	docker, err := stepDocker(ctx, opts)
	if err != nil {
		return err
	}
	if !docker.OK {
		return fmt.Errorf("preflight: Docker is not answering\n  start Docker Desktop, then run cardo up again")
	}

	busy, err := opts.Deps.BusyPorts("127.0.0.1", PublishPorts)
	if err != nil {
		return fmt.Errorf("preflight: cannot inspect ports: %w", err)
	}
	alreadyRunning := len(busy) > 0 && pingClickHouse(ctx, opts) == nil
	if len(busy) > 0 && !alreadyRunning {
		return fmt.Errorf("preflight: port %d is already taken\n  stop the process holding it, or run cardo up --hook-port 8099", busy[0])
	}

	secrets, err := stepSecrets(opts)
	if err != nil {
		return fmt.Errorf("secrets: %w", err)
	}
	stack, err := stepStack(ctx, opts, alreadyRunning)
	if err != nil {
		return fmt.Errorf("stack: %w", err)
	}

	rows := []Result{docker, {ID: "ports", Name: "PORTS", Value: portSummary(busy), OK: true}, secrets, stack}
	for _, id := range []string{"clickhouse", "grafana", "collector"} {
		r, err := RunStep(ctx, opts, id)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		rows = append(rows, r)
	}

	Render(opts.Out, rows)
	for _, r := range rows {
		if !r.OK {
			return fmt.Errorf("check %s failed: %s", r.Name, r.Fix)
		}
	}
	return nil
}

// Check is read-only: it never writes .env and never starts a container.
func Check(ctx context.Context, opts Options) ([]Result, error) {
	opts.defaults()

	docker, _ := stepDocker(ctx, opts)
	secrets, _ := stepSecretsReadOnly(opts)

	busy, _ := opts.Deps.BusyPorts("127.0.0.1", PublishPorts)
	ports := Result{ID: "ports", Name: "PORTS", Value: "5 of 5 listening", OK: true}
	if missing := missingPorts(busy); len(missing) > 0 {
		ports = Result{ID: "ports", Name: "PORTS", Value: "missing " + joinPorts(missing), Fix: "run cardo up"}
	}

	rows := []Result{docker, secrets, ports}
	for _, id := range []string{"clickhouse", "grafana", "collector"} {
		r, err := RunStep(ctx, opts, id)
		if err != nil {
			return rows, err
		}
		rows = append(rows, r)
	}

	// What the views actually read, taken from one real session. It is pending, not failed, until
	// the engineer has worked once; cardo check --proof makes it a failure.
	fields, err := stepFields(ctx, opts)
	if err != nil {
		return rows, err
	}
	rows = append(rows, fields)

	admin, err := checkAdminData(ctx, opts)
	if err != nil {
		return rows, err
	}
	rows = append(rows, admin)

	actors, err := checkActorTypes(ctx, opts)
	if err != nil {
		return rows, err
	}
	rows = append(rows, actors)

	gold, err := checkGoldAccess(ctx, opts)
	if err != nil {
		return rows, err
	}
	rows = append(rows, gold)
	return rows, nil
}

// stepSecretsReadOnly reports whether the secrets are usable without ever writing them.
func stepSecretsReadOnly(opts Options) (Result, error) {
	env, _ := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if problems := ValidateEnv(env); len(problems) > 0 {
		return Result{ID: "secrets", Name: "SECRETS", Value: problems[0], Fix: "run cardo up to generate a valid .env"}, nil
	}
	return Result{ID: "secrets", Name: "SECRETS", Value: ".env 0600, salt 64 hex", OK: true}, nil
}

// Render prints the board. Plain text on purpose: it survives pipes, logs and CI.
func Render(w interface{ Write([]byte) (int, error) }, rows []Result) {
	render(w, rows, false)
}

// RenderCheck is the read-only twin: the same table, but the closing line does not claim the install
// is verified, because a check never sends a probe.
func RenderCheck(w interface{ Write([]byte) (int, error) }, rows []Result) {
	render(w, rows, true)
}

func render(w interface{ Write([]byte) (int, error) }, rows []Result, checked bool) {
	title := "CARDO · INSTALLATION"
	if checked {
		title = "CARDO · CHECKS"
	}
	fmt.Fprintf(w, "\n%s\n\n", title)
	done, pending := 0, 0
	for i, r := range rows {
		state := "FAIL"
		if r.OK {
			state = "OK"
			done++
		}
		if r.Pending {
			state = "WAIT"
			pending++
		}
		fmt.Fprintf(w, "  %02d  %-12s %-38s %s\n", i+1, r.Name, r.Value, state)
		if !r.OK && !r.Pending {
			if r.Cause != "" {
				fmt.Fprintf(w, "      cause: %s\n", r.Cause)
			}
			if r.Fix != "" {
				fmt.Fprintf(w, "      → %s\n", r.Fix)
			}
		} else if r.Pending && r.Fix != "" {
			fmt.Fprintf(w, "      next: %s\n", r.Fix)
		}
	}
	fmt.Fprintf(w, "\n  %d / %d steps complete\n", done, len(rows))
	if done+pending != len(rows) {
		fmt.Fprintf(w, "  Complete the first failing step, then run cardo up again.\n\n")
	} else if checked && pending > 0 {
		fmt.Fprintf(w, "  Checks that can run pass; %d waits for real work. cardo check --proof requires it.\n\n", pending)
	} else if checked {
		fmt.Fprintf(w, "  All checks pass. They read state; data arrival is proved by cardo probe.\n\n")
	} else {
		fmt.Fprintf(w, "  Installed. Verification waits for your first session (cardo check --proof). Updates: cardo up\n\n")
	}
}

// --- small helpers ----------------------------------------------------------

func requireComposeFile(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		return fmt.Errorf("no docker-compose.yml in %s: run cardo from the repository root, or pass --dir", dir)
	}
	return nil
}

func dockerVersion(ctx context.Context, opts Options) (string, error) {
	out, err := opts.Deps.Exec(ctx, opts.Dir, "docker", "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		if out != "" {
			return "", fmt.Errorf("%s", firstLine(out))
		}
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.Contains(out, "ERROR") {
		return "", errors.New("empty answer from docker info")
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// serviceURL builds a loopback URL from parts. INV-7 forbids a host literal in the source, and
// composing the address here keeps the rule about data leaving the network visible rather than
// buried in a string.
func serviceURL(port, path string) string {
	u := url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", port), Path: path}
	return u.String()
}

func pingClickHouse(ctx context.Context, opts Options) error {
	return opts.Deps.WaitHTTP(ctx, serviceURL("8124", "/ping"), 2*time.Second)
}

func portSummary(busy []int) string {
	if len(busy) == 0 {
		return "5 free"
	}
	return fmt.Sprintf("%d already listening", len(busy))
}

func joinPorts(ports []int) string {
	var b strings.Builder
	for i, p := range ports {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprint(&b, p)
	}
	return b.String()
}

func containsPort(ports []int, want int) bool {
	for _, p := range ports {
		if p == want {
			return true
		}
	}
	return false
}

func missingPorts(busy []int) []int {
	has := map[int]bool{}
	for _, p := range busy {
		has[p] = true
	}
	var missing []int
	for _, p := range PublishPorts {
		if !has[p] {
			missing = append(missing, p)
		}
	}
	return missing
}
