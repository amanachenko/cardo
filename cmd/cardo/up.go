package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/amanachenko/cardo/internal/launcher"
)

// runUp is the quickstart: one command from nothing to a verified stack. It is safe to run again —
// the second run repairs and says what it kept. With --web it also serves the board, where every
// button runs the same step the CLI ran.
func runUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	useSynopsis(fs, "up")
	var (
		dir    = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
		pilot  = fs.Bool("pilot", false, "prepare the pilot rows: read-only leader logins, settings bundle, engineer notice")
		wait   = fs.Duration("wait", 90*time.Second, "how long to wait for ClickHouse after the stack starts")
		dryRun = fs.Bool("dry-run", false, "say what would change, write nothing")
		web    = fs.Bool("web", false, "serve the board on loopback after the run")
		port   = fs.Int("port", 8484, "port for the board")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := launcher.Options{Dir: *dir, Pilot: *pilot, Wait: *wait, DryRun: *dryRun, Out: os.Stdout}
	if err := launcher.Up(ctx, opts); err != nil {
		return err
	}

	// The state file is a cache, not a source of truth. Losing it costs a re-check.
	st := launcher.LoadState(launcherStatePath())
	if *pilot {
		st.Kind = "pilot"
	} else {
		st.Kind = "solo"
	}
	st.Mark("up", "completed", true)
	if err := st.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "cardo: could not save launcher state: %v\n", err)
	}

	if *web {
		fmt.Println()
		return runUI(ctx, opts, *port, true)
	}
	fmt.Println("  Next: open 127.0.0.1:3001 for the dashboards, or run `cardo check`.")
	return nil
}

// runUI serves the board without running anything first, so the browser is the only thing a person
// needs to open on a stack that is already up.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func runUI(ctx context.Context, opts launcher.Options, port int, openBrowser bool) error {
	token, err := boardToken(port)
	if err != nil {
		return err
	}
	opts.Token = token
	srv, err := launcher.NewServer(opts)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	url := srv.URL(addr)

	// The port is taken before anything is printed: a URL that was not served is worse than no URL.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) && boardAnswers(addr, token) {
			fmt.Printf("  Board already running: %s\n", url)
			return nil
		}
		return fmt.Errorf("port %d is used by another process; run \"cardo ui --port %d\", or stop the process holding it", port, port+1)
	}
	defer ln.Close()

	fmt.Printf("  Board: %s\n  Ctrl-C stops the board. The stack keeps running.\n", url)
	// Open only for a person at a terminal: piping the output to a file or CI must not launch a
	// browser, and the URL is printed either way.
	if openBrowser && isTerminal(os.Stdout) {
		if err := launcher.OpenBrowser(url); err != nil {
			fmt.Printf("  (open the URL above manually: %v)\n", err)
		}
	}
	return srv.ServeListener(ctx, ln)
}

// boardToken keeps one token per port under the user cache, mode 0600, so a bookmarked link keeps
// working after a restart instead of turning into "this link is no longer valid".
func boardToken(port int) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "cardo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("token-%d", port))
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 32 {
			return t, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := launcher.RestrictFile(path); err != nil {
		return "", err
	}
	return token, nil
}

// boardAnswers tells "a Cardo board is already here" apart from "something else holds the port".
func boardAnswers(addr, token string) bool {
	u := url.URL{Scheme: "http", Host: addr, Path: "/api/state"}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Cardo-Token", token)
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

// runServe is the `cardo ui` command: the board on its own.
func runServe(args []string) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	useSynopsis(fs, "ui")
	var (
		dir    = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
		port   = fs.Int("port", 8484, "port for the board")
		noOpen = fs.Bool("no-open", false, "print the URL instead of opening a browser")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runUI(ctx, launcher.Options{Dir: *dir, Out: os.Stdout}, *port, !*noOpen)
}

// runSecret generates the salt and passwords into .env with mode 0600. It is the command the board
// shows on the secrets step, so copying it does what the step does.
func runSecret(args []string) error {
	fs := flag.NewFlagSet("secret", flag.ContinueOnError)
	useSynopsis(fs, "secret")
	dir := fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	res, err := launcher.RunStep(context.Background(), launcher.Options{Dir: *dir, Out: os.Stdout}, "secrets")
	if err != nil {
		return err
	}
	fmt.Printf("  %s: %s\n", res.Name, res.Value)
	if !res.OK {
		if res.Cause != "" {
			fmt.Printf("  cause: %s\n", res.Cause)
		}
		return fmt.Errorf("%s", res.Fix)
	}
	return nil
}

// runStepCommand runs one launcher step and prints its result, so the command shown on the board is
// the command that does the work.
func runStepCommand(step string, args []string) error {
	fs := flag.NewFlagSet(step, flag.ContinueOnError)
	dir := fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	res, err := launcher.RunStep(context.Background(), launcher.Options{Dir: *dir, Out: os.Stdout}, step)
	if err != nil {
		return err
	}
	fmt.Printf("  %s: %s\n", res.Name, res.Value)
	if !res.OK {
		if res.Cause != "" {
			fmt.Printf("  cause: %s\n", res.Cause)
		}
		return fmt.Errorf("%s", res.Fix)
	}
	return nil
}

// runConnect installs the Claude Code settings file into one project. It is the same step the board
// runs, exposed for scripts and for machines where the board is not used.
func runConnect(args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	useSynopsis(fs, "connect")
	var (
		dir     = fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
		project = fs.String("project", "", "project directory that receives .claude/settings.json (required)")
	)
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	if *project == "" {
		return fmt.Errorf("connect needs --project <directory>: the settings file is written there")
	}
	res, err := launcher.RunStep(context.Background(), launcher.Options{Dir: *dir, Project: *project, Out: os.Stdout}, "connect")
	if err != nil {
		return err
	}
	fmt.Printf("  %s: %s\n", res.Name, res.Value)
	if !res.OK {
		if res.Cause != "" {
			fmt.Printf("  cause: %s\n", res.Cause)
		}
		return fmt.Errorf("%s", res.Fix)
	}
	return nil
}

// runCheck is the contract the board renders: the same checks, non-interactive, read-only.
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	useSynopsis(fs, "check")
	dir := fs.String("dir", filepath.Join("deploy", "compose"), "directory holding docker-compose.yml and .env")
	proof := fs.Bool("proof", false, "fail unless one real session has the fields the views read")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rows, err := launcher.Check(ctx, launcher.Options{Dir: *dir, Out: os.Stdout})
	if err != nil {
		return err
	}
	launcher.RenderCheck(os.Stdout, rows)
	var failed int
	for _, r := range rows {
		if r.Pending && !*proof {
			continue
		}
		if !r.OK {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(rows))
	}
	return nil
}

// launcherStatePath keeps the cache out of the repository: it describes this machine, not the code.
func launcherStatePath() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "cardo", "launcher.json")
}
