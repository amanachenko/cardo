package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// stepHistory imports existing Claude Code transcripts once and installs the watcher that keeps
// doing it. It reads numbers only; no prompt text is stored. Both halves are idempotent.
func stepHistory(ctx context.Context, opts Options) (Result, error) {
	bin, err := os.Executable()
	if err != nil {
		return Result{ID: "history", Name: "Your history", Cause: err.Error(), Fix: "Run cardo import -write by hand."}, err
	}
	env := os.Environ()
	if dot, err := ReadDotEnv(filepath.Join(opts.Dir, ".env")); err == nil {
		add := func(key, name string) {
			if v := dot[key]; v != "" {
				env = append(env, name+"="+v)
			}
		}
		add("CARDO_SALT", "CARDO_SALT")
		add("CLICKHOUSE_USER", "CARDO_CLICKHOUSE_USER")
		add("CLICKHOUSE_PASSWORD", "CARDO_CLICKHOUSE_PASSWORD")
	}
	env = append(env, "CARDO_CLICKHOUSE_URL=http"+"://"+"127.0.0.1:8124")

	email := gitUserEmail()
	if email == "" {
		email = os.Getenv("USER") + "@localhost"
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude", "projects")

	imp := exec.CommandContext(ctx, bin, "import", "-write", "-dir", dir, "-email", email)
	imp.Env = env
	out, err := imp.CombinedOutput()
	if err != nil {
		return Result{
			ID: "history", Name: "Your history",
			Cause: strings.TrimSpace(string(out)) + " " + err.Error(),
			Fix:   "Run cardo import -write by hand to see the full report.",
		}, err
	}
	written := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "WROTE") {
			written = strings.TrimSpace(strings.TrimPrefix(line, "WROTE"))
		}
	}

	svc := exec.CommandContext(ctx, bin, "watch", "-install", "-dir", dir, "-email", email)
	svc.Env = env
	sout, err := svc.CombinedOutput()
	if err != nil {
		return Result{
			ID: "history", Name: "Your history",
			Cause: strings.TrimSpace(string(sout)) + " " + err.Error(),
			Fix:   "Install the watcher by hand: cardo watch -install",
		}, err
	}
	res := Result{ID: "history", Name: "Your history", OK: true}
	if written != "" {
		res.Value = written + " · watcher installed"
	} else {
		res.Value = "watcher installed"
	}
	return res, nil
}

func gitUserEmail() string {
	out, err := exec.Command("git", "config", "--global", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
