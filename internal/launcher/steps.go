package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Meta is what the board shows for a step: its place, its wording, and the command it will run.
// Result is what happened. The two are separate so the CLI can stay terse while the board carries
// the explanation.
type Meta struct {
	ID     string
	Group  string
	Name   string
	Cmd    string
	Title  string
	Body   []string
	KV     [][2]string
	Action string
	// Commands overrides Cmd per GOOS, for steps whose diagnostic tool differs by platform.
	Commands map[string]string
	Pilot    bool
	Planned  bool
	Optional bool   // diagnostics: shown and runnable, never blocks completion
	OptIn    bool   // the person's choice: off until asked for, and never part of "install all"
	Verify   bool   // the proof: can only pass after real work, and gates the "verified" claim
	Input    string // when set, the board asks for one value before the step can run
}

// Steps is the whole installation, in order. One list feeds the CLI, the board and the tests.
var Steps = []Meta{
	{
		ID: "docker", Group: "Machine", Name: "Docker daemon", Cmd: "docker info --format '{{.ServerVersion}}'",
		Title: "Requirement",
		Body: []string{
			"Verifies that the Docker daemon responds and that Compose is version 2 or later. When the daemon is unavailable, this step reports the reason and the command that starts it.",
		},
		Action: "Check",
	},
	{
		ID: "ports", Group: "Machine", Name: "Network ports", Cmd: "lsof -nP -iTCP:8124,9001,3001,4319,8089 -sTCP:LISTEN",
		Commands: map[string]string{
			"windows": `powershell -NoProfile -Command "Get-NetTCPConnection -LocalPort 8124,9001,3001,4319,8089 -State Listen | Select-Object LocalPort,OwningProcess"`,
		},
		Title: "Requirement",
		Body: []string{
			"The reference stack binds five loopback ports: 8124 and 9001 for ClickHouse, 3001 for Grafana, 4319 for OTLP and 8089 for hooks. A port in use is reported together with the process that holds it.",
		},
		Action: "Check",
	},
	{
		ID: "secrets", Group: "Machine", Name: "Secrets", Cmd: "cardo secret",
		Title: "Generated once, on this machine",
		Body: []string{
			"The pseudonymisation salt and two passwords are generated locally and written to deploy/compose/.env with mode 0600. The passwords are shown once, right after they are generated, and never again after a reload. The salt must be hexadecimal and at least 32 characters; without it the collector rejects every OTLP batch, and this step reports that refusal.",
			"A second run does not rotate the salt. Rotation would split one engineer into two pseudonyms.",
		},
		KV: [][2]string{
			{"CARDO_SALT", "generated · 64 hex characters"},
			{"CLICKHOUSE_PASSWORD", "generated · mode 0600"},
			{"GRAFANA_PASSWORD", "generated · mode 0600"},
			{"Salt custody", "the operator; in a pilot, the security team"},
		},
		Action: "Generate and write",
	},
	{
		ID: "stack", Group: "Services", Name: "Reference stack", Cmd: "docker compose up -d --wait",
		Title: "Services started",
		Body: []string{
			"ClickHouse, Grafana, the collector and a one-shot migration. The schema is embedded in the image, so an update requires the same command and no separate migration step.",
		},
		KV: [][2]string{
			{"ClickHouse", "127.0.0.1:8124 · 9001"},
			{"Grafana", "127.0.0.1:3001"},
			{"Collector", "OTLP 4319 · hooks 8089"},
			{"Migration", "one-shot, waits for the collector"},
		},
		Action: "Start",
	},
	{
		ID: "clickhouse", Group: "Data", Name: "ClickHouse", Cmd: "curl -s 127.0.0.1:8124/ping",
		Title: "First link in the chain",
		Body: []string{
			"The server answers a ping. This does not yet prove that data arrived, but no later check is meaningful without it.",
		},
		Action: "Check",
	},
	{
		ID: "grafana", Group: "Data", Name: "Grafana", Cmd: "curl -s 127.0.0.1:3001/api/health",
		Title: "Second link in the chain",
		Body: []string{
			"Grafana is healthy and can reach the database. Dashboards are provisioned from the repository; none are configured by hand.",
		},
		Action: "Check",
	},
	{
		ID: "collector", Group: "Data", Name: "Collector", Cmd: "nc -z 127.0.0.1 4319",
		Commands: map[string]string{
			"windows": `powershell -NoProfile -Command "Test-NetConnection 127.0.0.1 -Port 4319 -InformationLevel Quiet"`,
		},
		Title: "Third link in the chain",
		Body: []string{
			"The collector accepts OTLP on 4319 and hooks on 8089. Each of the twelve hooks has a one-second timeout: when the collector is unreachable, the engineer's session continues.",
		},
		Action: "Check",
	},
	{
		ID: "data", Group: "Data", Name: "Data arrived", Cmd: "cardo probe", Optional: true,
		Title: "Proves the pipeline, not the ports",
		Body: []string{
			"One OTLP log and one hook event are sent with a marker unique to this run, then read back from ClickHouse by that marker. A row that appears here is the end of the chain: settings, collector, database.",
			"The exporter flushes on a ten-second timer, so this step polls for up to 45 seconds before reporting a failure.",
		},
		Action: "Send a probe",
	},
	{
		ID: "gold", Group: "Access", Name: "Leader access", Cmd: "cardo gold",
		Title: "Summaries without personal rows",
		Body: []string{
			"Creates the gold-only ClickHouse user the leadership Grafana uses. It can read the nine gold views and nothing else: bronze and silver are refused, and the step proves that refusal live before it passes.",
			"A refusal is the successful outcome. If the read-only user can read a raw table, this step fails and the login must not be handed out.",
		},
		Action: "Verify",
	},
	{
		ID: "connect", Group: "Engineers", Name: "Claude Code settings", Cmd: "cardo connect --project <directory>",
		Title: "Installed once, without per-session flags", Input: "project",
		Body: []string{
			"The settings file is written to the selected project. The engineer then works normally: no session is labelled, no flag is added to the command line, and no habit is required.",
			"For a team, the same file is distributed through MDM, GPO or Intune once, with no agent installed on developer machines.",
		},
		KV: [][2]string{
			{"enabled", "command, subagent and MCP names · duration · tool errors · version"},
			{"disabled", "prompt and response text · file contents · tool parameters"},
			{"outbound", "0 bytes"},
		},
		Action: "Install",
	},
	{
		ID: "history", Group: "Data", Name: "Your history", Cmd: "cardo import -write", OptIn: true,
		Title: "One-time import, if you ask for it",
		Body: []string{
			"Imports the Claude Code sessions already on this machine, once. Only counts, tokens and timing are stored; prompt text is measured and discarded. Nothing runs in the background afterwards: no watcher, no service, nothing left behind.",
			"It is off until you tick it, because it reads files outside the stack. The import is idempotent: running it again replaces the same sessions instead of duplicating them, and sessions that already arrived through live telemetry are skipped, so nothing is counted twice.",
		},
		Action: "Import",
	},
	{
		ID: "session", Group: "Engineers", Name: "Your session", Cmd: "cardo session", Verify: true,
		Title: "The row you will recognise",
		Body: []string{
			"Looks for hook events the installer did not send, so the first real Claude Code session appears here. It never blocks the install summary; it is what turns installed into verified, and it can only pass after you have worked normally once.",
		},
		Action: "Check",
	},
	{
		ID: "bundle", Group: "Engineers", Name: "Managed settings bundle", Cmd: "MDM: managed-settings.json",
		Title: "One file, no installation", Pilot: true, Planned: true,
		Body: []string{
			"The same JSON as for a single engineer, with the collector address inside the organization's network. No daemon, no script and no extension. An engineer can read every line: this is the entire client.",
		},
		Action: "Record as deployed",
	},
	{
		ID: "notice", Group: "Engineers", Name: "Engineer notice", Cmd: "cardo notice --format md",
		Title: "What the organization must disclose", Pilot: true, Planned: true,
		Body: []string{
			"Who holds the salt. Which settings are enabled. That the data is not used for individual performance decisions. One page, without legal language, using the same terms as the settings file.",
		},
		Action: "Create",
	},
}

// MetaByID returns the step, or false.
func MetaByID(id string) (Meta, bool) {
	for _, m := range Steps {
		if m.ID == id {
			return m, true
		}
	}
	return Meta{}, false
}

// RunStep executes one step. It is the only entry point the board and the CLI use, so a button and
// a terminal command cannot drift apart.
func RunStep(ctx context.Context, opts Options, id string) (Result, error) {
	opts.defaults()
	switch id {
	case "docker":
		return stepDocker(ctx, opts)
	case "ports":
		return stepPorts(ctx, opts)
	case "secrets":
		return stepSecrets(opts)
	case "stack":
		return stepStack(ctx, opts, false)
	case "clickhouse":
		return stepClickHouse(ctx, opts)
	case "grafana":
		return stepGrafana(ctx, opts)
	case "data":
		return stepData(ctx, opts)
	case "history":
		return stepHistory(ctx, opts)
	case "session":
		return stepSession(ctx, opts)
	case "gold":
		return stepGrafanaRO(ctx, opts)
	case "fields":
		return stepFields(ctx, opts)
	case "collector":
		return stepCollector(ctx, opts)
	case "connect":
		return stepConnect(opts)
	case "grafana_ro", "bundle", "notice":
		return Result{ID: id, Name: "PILOT", Value: "not implemented", Fix: "this row is described in docs/design/quickstart-launcher.md and is not built yet"}, nil
	}
	return Result{}, fmt.Errorf("unknown step %q", id)
}

func stepDocker(ctx context.Context, opts Options) (Result, error) {
	info := inspectDocker(ctx, opts)
	switch info.State {
	case dockerReady:
		return Result{ID: "docker", Name: "DOCKER", Value: "v" + info.Server + " · compose " + info.Compose, OK: true}, nil
	case composeOld:
		// The stack can still be started without `up --wait`; the next step falls back and says so.
		return Result{ID: "docker", Name: "DOCKER", Value: "v" + info.Server + " · compose " + info.Compose + " (no --wait)", OK: true}, nil
	default:
		return Result{ID: "docker", Name: "DOCKER", Value: "not ready", Cause: info.Cause, Fix: info.Fix}, nil
	}
}

func stepPorts(ctx context.Context, opts Options) (Result, error) {
	busy, err := opts.Deps.BusyPorts("127.0.0.1", PublishPorts)
	if err != nil {
		return Result{
			ID: "ports", Name: "PORTS", Value: "cannot inspect ports",
			Cause: "The ports could not be inspected: " + err.Error(),
			Fix:   "Check that the current user may open a listening socket on loopback.",
		}, nil
	}
	if len(busy) == 0 {
		return Result{ID: "ports", Name: "PORTS", Value: "5 free", OK: true}, nil
	}
	// All five ports held is what an installed stack looks like, not a failure: a second run must
	// repair, not refuse. The ping separates "our stack is running" from "something else took them".
	if len(busy) == len(PublishPorts) {
		if err := opts.Deps.WaitHTTP(ctx, serviceURL("8124", "/ping"), 2*time.Second); err == nil {
			return Result{ID: "ports", Name: "PORTS", Value: "stack already running on " + joinPorts(busy), OK: true}, nil
		}
	}
	cause := "Something already listens on " + joinPorts(busy) + ", so the stack cannot bind these ports."
	if owner := portOwner(opts.goos(), busy[0]); owner != "" {
		cause += " The first of them is held by " + owner + "."
	}
	return Result{
		ID: "ports", Name: "PORTS", Value: "ports in use: " + joinPorts(busy),
		Cause: cause,
		Fix:   "Run `" + portOwnerCommand(opts.goos(), busy[0]) + "` to see the process, stop it, then retry.",
	}, nil
}

func stepSecrets(opts Options) (Result, error) {
	path := filepath.Join(opts.Dir, ".env")
	env, err := ReadDotEnv(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{
			ID: "secrets", Name: "SECRETS", Value: "cannot read .env",
			Cause: "The existing " + path + " could not be read: " + err.Error(),
			Fix:   "Correct the file permissions, or move the file aside and run this step again.",
		}, nil
	}
	if problems := ValidateEnv(env); len(problems) == 0 {
		return Result{ID: "secrets", Name: "SECRETS", Value: ".env kept, salt unchanged", OK: true}, nil
	} else {
		s, err := GenerateSecrets(cryptoReader{})
		if err != nil {
			return Result{
				ID: "secrets", Name: "SECRETS", Value: "cannot generate secrets",
				Cause: "Random bytes were unavailable: " + err.Error(),
				Fix:   "Retry. If the error repeats, the system entropy source is unavailable.",
			}, nil
		}
		if err := WriteDotEnv(path, env, s); err != nil {
			return Result{
				ID: "secrets", Name: "SECRETS", Value: "cannot write .env",
				Cause: "Writing " + path + " failed: " + err.Error(),
				Fix:   "Check that the directory is writable and that no other process holds the file.",
			}, nil
		}
		user := env["GRAFANA_USER"]
		if user == "" {
			user = "admin"
		}
		return Result{
			ID: "secrets", Name: "SECRETS", Value: ".env 0600, salt 64 hex", OK: true,
			Reveal: map[string]string{
				"Grafana user":        user,
				"Grafana password":    s.GrafanaPassword,
				"ClickHouse password": s.ClickHousePassword,
			},
		}, nil
	}
}

func stepStack(ctx context.Context, opts Options, alreadyRunning bool) (Result, error) {
	if alreadyRunning {
		return Result{ID: "stack", Name: "STACK", Value: "already running", OK: true}, nil
	}
	if opts.DryRun {
		return Result{ID: "stack", Name: "STACK", Value: "dry run: would start", OK: true}, nil
	}

	waitFlag, composeVersion := composeSupportsWait(ctx, opts)
	base := []string{"compose"}
	// The Admin API path is optional: its profile starts only when an organization key exists.
	if env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env")); err == nil && strings.TrimSpace(env["CARDO_ADMIN_KEY"]) != "" {
		base = append(base, "--profile", "admin")
	}
	args := append(append([]string{}, base...), "up", "-d")
	if waitFlag {
		args = append(args, "--wait")
	}
	out, err := opts.Deps.Exec(ctx, opts.Dir, "docker", args...)
	if err != nil && waitFlag && looksLikeUnknownWait(out+err.Error()) {
		// Compose answered a version that supports --wait but the flag is not there: older plugin.
		waitFlag = false
		args = append(append([]string{}, base...), "up", "-d")
		out, err = opts.Deps.Exec(ctx, opts.Dir, "docker", args...)
	}
	if err != nil {
		cause := firstLine(out)
		if cause == "" {
			cause = err.Error()
		}
		return Result{
			ID: "stack", Name: "STACK", Value: "compose up failed",
			Cause: "docker compose up returned: " + cause,
			Fix:   "Run docker compose logs migrate in " + opts.Dir + ". The data volume is kept; fix the reported cause and run this step again.",
		}, nil
	}

	// Without --wait, Compose returns as soon as the containers are created; wait for ClickHouse
	// ourselves. With --wait it already waited for the healthcheck, so this is a short confirmation.
	if err := opts.Deps.WaitHTTP(ctx, serviceURL("8124", "/ping"), opts.Wait); err != nil {
		return Result{
			ID: "stack", Name: "STACK", Value: "ClickHouse did not answer",
			Cause: "The containers started, but ClickHouse did not answer within " + opts.Wait.String() + ": " + err.Error(),
			Fix:   "Run docker compose logs clickhouse. On a fresh volume the first start can take longer; run this step again before changing anything.",
		}, nil
	}

	value := "clickhouse grafana collector"
	if !waitFlag {
		value += " (compose " + composeVersion + ", no --wait)"
	}
	return Result{ID: "stack", Name: "STACK", Value: value, OK: true}, nil
}

func looksLikeUnknownWait(s string) bool {
	return strings.Contains(s, "unknown flag") && strings.Contains(s, "wait")
}

func stepClickHouse(ctx context.Context, opts Options) (Result, error) {
	if err := pingClickHouse(ctx, opts); err != nil {
		return Result{
			ID: "clickhouse", Name: "CLICKHOUSE", Value: "no answer on 8124",
			Cause: "No HTTP answer from 127.0.0.1:8124/ping: " + err.Error(),
			Fix:   "Run docker compose logs clickhouse. If the port is held by another process, step 02 names it.",
		}, nil
	}
	return Result{ID: "clickhouse", Name: "CLICKHOUSE", Value: "ping ok on 8124", OK: true}, nil
}

func stepGrafana(ctx context.Context, opts Options) (Result, error) {
	if err := opts.Deps.WaitHTTP(ctx, serviceURL("3001", "/api/health"), 5*time.Second); err != nil {
		return Result{
			ID: "grafana", Name: "GRAFANA", Value: "no health answer on 3001",
			Cause: "No healthy answer from 127.0.0.1:3001/api/health: " + err.Error(),
			Fix:   "Run docker compose logs grafana. The datasource is provisioned from the repository, so a failure here is a container or port problem, not configuration.",
		}, nil
	}
	return Result{ID: "grafana", Name: "GRAFANA", Value: "health ok on 3001", OK: true}, nil
}

func stepCollector(ctx context.Context, opts Options) (Result, error) {
	busy, err := opts.Deps.BusyPorts("127.0.0.1", []int{4319})
	if err != nil {
		return Result{
			ID: "collector", Name: "COLLECTOR", Value: "cannot inspect port 4319",
			Cause: "The port could not be inspected: " + err.Error(),
			Fix:   "Check that the current user may open a listening socket on loopback.",
		}, nil
	}
	if containsPort(busy, 4319) {
		return Result{ID: "collector", Name: "COLLECTOR", Value: "accepting on 4319", OK: true}, nil
	}
	return Result{
		ID: "collector", Name: "COLLECTOR", Value: "nothing on 4319",
		Cause: "Nothing listens on 127.0.0.1:4319, so Claude Code would have nowhere to send telemetry.",
		Fix:   "Check that the collector container is running and that CARDO_SALT in deploy/compose/.env is at least 32 hexadecimal characters: without it every OTLP batch is refused. The collector keeps no log of its own, so the salt step is the place to look."}, nil
}

// stepConnect installs the settings file into the selected project. Without a project it reports
// what is missing rather than guessing a directory.
func stepConnect(opts Options) (Result, error) {
	if opts.Project == "" {
		return Result{
			ID: "connect", Name: "CONNECT", Value: "no project selected",
			Cause: "No project directory was provided, so there is nowhere to write .claude/settings.json.",
			Fix:   "Enter the path of the project that should send telemetry, then run this step again.",
		}, nil
	}
	info, err := os.Stat(opts.Project)
	if err != nil {
		return Result{
			ID: "connect", Name: "CONNECT", Value: "project directory not found",
			Cause: opts.Project + " cannot be read: " + err.Error(),
			Fix:   "Enter an existing directory. The file is written to <directory>/.claude/settings.json.",
		}, nil
	}
	if !info.IsDir() {
		return Result{
			ID: "connect", Name: "CONNECT", Value: "not a directory",
			Cause: opts.Project + " is a file.",
			Fix:   "Enter the project directory, not a file inside it.",
		}, nil
	}
	bundle, err := SettingsBundle()
	if err != nil {
		return Result{
			ID: "connect", Name: "CONNECT", Value: "cannot build settings",
			Cause: "The settings bundle could not be generated: " + err.Error(),
			Fix:   "This is a defect in the binary; report it with the message above.",
		}, nil
	}
	path, err := InstallSettings(opts.Project, bundle)
	if err != nil {
		return Result{
			ID: "connect", Name: "CONNECT", Value: "cannot write settings",
			Cause: "Writing " + filepath.Join(opts.Project, ".claude", "settings.json") + " failed: " + err.Error(),
			Fix:   "Check that the directory is writable and that .claude is not read-only.",
		}, nil
	}
	return Result{ID: "connect", Name: "CONNECT", Value: "settings installed: " + path, OK: true}, nil
}
