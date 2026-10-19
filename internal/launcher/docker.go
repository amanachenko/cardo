package launcher

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// dockerState is what the first step must tell apart, because the remedy differs: a missing binary,
// a daemon that is not running, and an old Compose are three different problems that all look like
// "docker did not work" in a bare error message.
type dockerState int

const (
	dockerAbsent dockerState = iota
	daemonDown
	composeOld
	dockerReady
)

type dockerInfo struct {
	State   dockerState
	Server  string // server version, when known
	Compose string // compose version, when known
	Cause   string
	Fix     string
}

// minComposeMajor/minor are the first Compose release that supports `up --wait`.
const (
	minComposeMajor = 2
	minComposeMinor = 17
)

func inspectDocker(ctx context.Context, opts Options) dockerInfo {
	goos := opts.goos()

	if _, err := opts.Deps.Exec(ctx, opts.Dir, "docker", "version", "--format", "{{.Client.Version}}"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return dockerInfo{
				State: dockerAbsent,
				Cause: "Docker is not installed on this machine.",
				Fix:   dockerInstallHint(goos),
			}
		}
		return dockerInfo{
			State: daemonDown,
			Cause: "The docker command is present but did not answer: " + err.Error(),
			Fix:   dockerStartHint(goos),
		}
	}

	server, err := opts.Deps.Exec(ctx, opts.Dir, "docker", "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		cause := firstLine(server)
		if cause == "" {
			cause = err.Error()
		}
		return dockerInfo{
			State: daemonDown,
			Cause: "The Docker daemon did not respond: " + cause,
			Fix:   dockerStartHint(goos),
		}
	}

	compose, err := opts.Deps.Exec(ctx, opts.Dir, "docker", "compose", "version", "--short")
	if err != nil {
		return dockerInfo{
			State:  composeOld,
			Server: strings.TrimSpace(server),
			Cause:  "Docker Compose v2 does not answer: " + err.Error(),
			Fix:    composeUpgradeHint(goos, ""),
		}
	}
	compose = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(compose), "v"))

	if major, minor, ok := parseComposeVersion(compose); ok && (major < minComposeMajor || (major == minComposeMajor && minor < minComposeMinor)) {
		return dockerInfo{
			State:   composeOld,
			Server:  strings.TrimSpace(server),
			Compose: compose,
			Cause:   fmt.Sprintf("Compose %s is older than v%d.%d, which is when `up --wait` was added.", compose, minComposeMajor, minComposeMinor),
			Fix:     composeUpgradeHint(goos, compose),
		}
	}

	return dockerInfo{State: dockerReady, Server: strings.TrimSpace(server), Compose: compose}
}

func parseComposeVersion(v string) (major, minor int, ok bool) {
	m := regexp.MustCompile(`^(\d+)\.(\d+)`).FindStringSubmatch(v)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

// composeSupportsWait reports whether `docker compose up --wait` can be used, and the version found.
func composeSupportsWait(ctx context.Context, opts Options) (bool, string) {
	out, err := opts.Deps.Exec(ctx, opts.Dir, "docker", "compose", "version", "--short")
	if err != nil {
		return false, ""
	}
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "v"))
	major, minor, ok := parseComposeVersion(v)
	if !ok {
		return false, v
	}
	return major > minComposeMajor || (major == minComposeMajor && minor >= minComposeMinor), v
}

// dockerInstallHint names the exact command for this platform. A link is not a command, so both are
// given: the one-line install where a package manager exists, and the page that always works:
//
// The printed text omits the scheme so that the INV-7 scan stays meaningful: this package makes no
// outbound call, and a bare host is a reference a person can read.
func dockerInstallHint(goos string) string {
	switch goos {
	case "darwin":
		return "Install Docker Desktop: `brew install --cask docker`, or download it from docs.docker.com/desktop/install/mac-install. Then start it and run this step again."
	case "windows":
		return "Install Docker Desktop: `winget install --id Docker.DockerDesktop -e`, or download it from docs.docker.com/desktop/install/windows-install. Then start it and run this step again."
	default:
		return "Install Docker Engine from docs.docker.com/engine/install (or your distribution's docker.io package). Add your user to the docker group, or run the installer with sudo. Then run this step again."
	}
}

// dockerStartHint starts the daemon that is installed but not running.
func dockerStartHint(goos string) string {
	switch goos {
	case "darwin":
		return "Start Docker Desktop (`open -a Docker`) and wait until it reports that the engine is running, then run this step again."
	case "windows":
		return "Start Docker Desktop (`Start-Process 'Docker Desktop'`) and wait until the whale icon stops animating, then run this step again."
	default:
		return "Start the daemon: `sudo systemctl start docker` (and `sudo systemctl enable docker` if it should start with the machine). Then run this step again."
	}
}

func composeUpgradeHint(goos, found string) string {
	current := "Docker Compose v2 is missing or too old."
	if found != "" {
		current = "Docker Compose " + found + " is older than v2.17."
	}
	switch goos {
	case "darwin", "windows":
		return current + " Docker Desktop bundles a current Compose; updating Docker Desktop is the shortest fix. The installer can still continue without `--wait`."
	default:
		return current + " Install the Compose plugin from docs.docker.com/compose/install/linux. The installer can still continue without `--wait`."
	}
}
