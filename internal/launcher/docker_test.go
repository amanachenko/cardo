package launcher

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// execScript answers docker commands the way a machine would, so the preflight can be asserted for
// all three platforms without one.
func execScript(handler func(args []string) (string, error)) func(context.Context, string, string, ...string) (string, error) {
	return func(ctx context.Context, dir, name string, args ...string) (string, error) {
		return handler(append([]string{name}, args...))
	}
}

func TestDockerPreflightOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		goos      string
		handler   func(args []string) (string, error)
		wantState dockerState
		wantCause string
		wantFix   string
	}{
		{
			name: "absent on macOS",
			goos: "darwin",
			handler: func(args []string) (string, error) {
				return "", &exec.Error{Name: "docker", Err: exec.ErrNotFound}
			},
			wantState: dockerAbsent,
			wantCause: "not installed",
			wantFix:   "brew install --cask docker",
		},
		{
			name: "absent on Windows",
			goos: "windows",
			handler: func(args []string) (string, error) {
				return "", &exec.Error{Name: "docker", Err: exec.ErrNotFound}
			},
			wantState: dockerAbsent,
			wantFix:   "winget install --id Docker.DockerDesktop",
		},
		{
			name: "absent on Linux",
			goos: "linux",
			handler: func(args []string) (string, error) {
				return "", &exec.Error{Name: "docker", Err: exec.ErrNotFound}
			},
			wantState: dockerAbsent,
			wantFix:   "docs.docker.com/engine/install",
		},
		{
			name: "daemon down on macOS",
			goos: "darwin",
			handler: func(args []string) (string, error) {
				if len(args) > 1 && args[1] == "version" {
					return "29.0.0", nil
				}
				return "Cannot connect to the Docker daemon at unix:///var/run/docker.sock", errors.New("exit status 1")
			},
			wantState: daemonDown,
			wantFix:   "open -a Docker",
		},
		{
			name: "daemon down on Linux",
			goos: "linux",
			handler: func(args []string) (string, error) {
				if len(args) > 1 && args[1] == "version" {
					return "29.0.0", nil
				}
				return "permission denied", errors.New("exit status 1")
			},
			wantState: daemonDown,
			wantFix:   "systemctl start docker",
		},
		{
			name: "daemon down on Windows",
			goos: "windows",
			handler: func(args []string) (string, error) {
				if len(args) > 1 && args[1] == "version" {
					return "29.0.0", nil
				}
				return "", errors.New("pipe not found")
			},
			wantState: daemonDown,
			wantFix:   "Start-Process 'Docker Desktop'",
		},
		{
			name: "compose too old",
			goos: "darwin",
			handler: func(args []string) (string, error) {
				switch {
				case args[1] == "version":
					return "29.0.0", nil
				case args[1] == "info":
					return "29.0.0", nil
				default:
					return "2.10.0", nil
				}
			},
			wantState: composeOld,
			wantCause: "older than v2.17",
		},
		{
			name: "ready",
			goos: "linux",
			handler: func(args []string) (string, error) {
				switch {
				case args[1] == "version":
					return "29.0.0", nil
				case args[1] == "info":
					return "29.6.1", nil
				default:
					return "v5.2.0", nil
				}
			},
			wantState: dockerReady,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Dir: t.TempDir(), GOOS: tc.goos}
			opts.Deps.Exec = execScript(tc.handler)
			info := inspectDocker(context.Background(), opts)
			if info.State != tc.wantState {
				t.Fatalf("state = %v, want %v (cause %q)", info.State, tc.wantState, info.Cause)
			}
			if tc.wantCause != "" && !strings.Contains(info.Cause, tc.wantCause) {
				t.Errorf("cause = %q, want it to contain %q", info.Cause, tc.wantCause)
			}
			if tc.wantFix != "" && !strings.Contains(info.Fix, tc.wantFix) {
				t.Errorf("fix = %q, want it to contain %q", info.Fix, tc.wantFix)
			}
			if tc.wantState != dockerReady && info.Fix == "" {
				t.Error("a failure without a remedy is a dead end")
			}
		})
	}
}

func TestStackFallsBackWithoutWait(t *testing.T) {
	var calls []string
	opts := Options{Dir: t.TempDir(), Wait: time.Second}
	opts.Deps.Exec = execScript(func(args []string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[1] == "compose" && args[2] == "version" {
			return "2.10.0", nil
		}
		return "", nil
	})
	opts.Deps.WaitHTTP = func(ctx context.Context, url string, timeout time.Duration) error { return nil }

	res, err := stepStack(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("stack failed: %s / %s", res.Cause, res.Fix)
	}
	if !strings.Contains(res.Value, "no --wait") {
		t.Errorf("value = %q, want it to name the fallback", res.Value)
	}
	for _, c := range calls {
		if strings.Contains(c, "--wait") {
			t.Errorf("compose was called with --wait on an old Compose: %s", c)
		}
	}
}

func TestStackUsesWaitWhenAvailable(t *testing.T) {
	var calls []string
	opts := Options{Dir: t.TempDir(), Wait: time.Second}
	opts.Deps.Exec = execScript(func(args []string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[1] == "compose" && args[2] == "version" {
			return "v2.39.1", nil
		}
		return "", nil
	})
	opts.Deps.WaitHTTP = func(ctx context.Context, url string, timeout time.Duration) error { return nil }

	if _, err := stepStack(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	var sawWait bool
	for _, c := range calls {
		if strings.Contains(c, "up -d --build --wait") {
			sawWait = true
		}
	}
	if !sawWait {
		t.Fatalf("compose up --wait was not used on Compose 2.39: %v", calls)
	}
}

func TestPortOwnerCommandPerPlatform(t *testing.T) {
	cases := map[string]string{
		"darwin":  "lsof -nP -iTCP:8124 -sTCP:LISTEN",
		"linux":   "lsof -nP -iTCP:8124 -sTCP:LISTEN",
		"windows": "Get-NetTCPConnection -LocalPort 8124",
	}
	for goos, want := range cases {
		if got := portOwnerCommand(goos, 8124); !strings.Contains(got, want) {
			t.Errorf("%s: %q does not contain %q", goos, got, want)
		}
	}
}
