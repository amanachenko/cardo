package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// stepHistory imports existing Claude Code transcripts once, on the person's own machine, when they
// ask for it. It reads numbers only; no prompt text is stored. Nothing runs in the background: the
// import is a command, not a service.
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
	written, skipped := "", ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "WROTE") {
			written = strings.TrimSpace(strings.TrimPrefix(line, "WROTE"))
		}
		if strings.HasPrefix(line, "SKIPPED") {
			skipped = strings.TrimSpace(strings.TrimPrefix(line, "SKIPPED"))
		}
	}
	res := Result{ID: "history", Name: "Your history", OK: true}
	switch {
	case written != "" && skipped != "":
		res.Value = written + " · " + skipped
	case written != "":
		res.Value = written
	case skipped != "":
		res.Value = skipped
	default:
		res.Value = "imported"
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
