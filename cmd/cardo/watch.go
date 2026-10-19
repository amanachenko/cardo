package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/pricing"
	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/transcripts"
)

// runWatch keeps the local transcripts flowing in without being asked. It remembers the last end
// timestamp per session, so only new or extended sessions are written.
func runWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	useSynopsis(fs, "watch")
	dir := fs.String("dir", filepath.Join(homeDir(), ".claude", "projects"), "transcript root")
	email := fs.String("email", gitEmail(), "your address; hashed, never stored")
	every := fs.Duration("every", time.Minute, "scan interval")
	install := fs.Bool("install", false, "install a user service and exit")
	uninstall := fs.Bool("uninstall", false, "remove the user service and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *uninstall {
		return uninstallService()
	}
	if *install {
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		return installService(bin, *dir, *email)
	}
	salt := os.Getenv("CARDO_SALT")
	if salt == "" {
		return fmt.Errorf("CARDO_SALT is required (it lives in deploy/compose/.env)")
	}
	id, err := pseudonym.ForEmail(salt, "v1", *email)
	if err != nil {
		return err
	}
	table, err := pricing.Load(context.Background(), 24*time.Hour)
	if err != nil {
		return err
	}
	state := loadState()
	fmt.Printf("WATCHING  %s · as %s…\n", *dir, id.Value[:12])
	for {
		stats, err := transcripts.Scan(*dir)
		if err == nil {
			var fresh []*transcripts.Session
			for _, s := range stats.Sessions {
				prev, seen := state[s.ID]
				if !seen || s.End.After(prev) {
					fresh = append(fresh, s)
				}
			}
			if len(fresh) > 0 {
				if err := writeSessions(context.Background(), table, os.Stdout, id.Value, "imported", "v1", fresh); err != nil {
					fmt.Fprintln(os.Stderr, "watch:", err)
				} else {
					for _, s := range fresh {
						state[s.ID] = s.End
					}
					saveState(state)
				}
			}
		}
		time.Sleep(*every)
	}
}

func statePath() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "cardo", "import-state.json")
}

func loadState() map[string]time.Time {
	m := map[string]time.Time{}
	b, err := os.ReadFile(statePath())
	if err != nil {
		return m
	}
	raw := map[string]string{}
	if json.Unmarshal(b, &raw) != nil {
		return m
	}
	for k, v := range raw {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			m[k] = t
		}
	}
	return m
}

func saveState(m map[string]time.Time) {
	raw := map[string]string{}
	for k, v := range m {
		raw[k] = v.Format(time.RFC3339Nano)
	}
	b, _ := json.Marshal(raw)
	_ = os.MkdirAll(filepath.Dir(statePath()), 0o755)
	_ = os.WriteFile(statePath(), b, 0o644)
}

// installService makes the watcher survive logout: launchd on macOS, systemd --user on Linux.
func installService(bin, dir, email string) error {
	home := homeDir()
	env := map[string]string{
		"CARDO_SALT":                os.Getenv("CARDO_SALT"),
		"CARDO_CLICKHOUSE_URL":      envOr("CARDO_CLICKHOUSE_URL", "http"+"://"+"127.0.0.1:8124"),
		"CARDO_CLICKHOUSE_USER":     envOr("CARDO_CLICKHOUSE_USER", "default"),
		"CARDO_CLICKHOUSE_PASSWORD": os.Getenv("CARDO_CLICKHOUSE_PASSWORD"),
	}
	switch runtime.GOOS {
	case "darwin":
		plist := filepath.Join(home, "Library", "LaunchAgents", "com.cardo.watch.plist")
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.cardo.watch</string>
<key>ProgramArguments</key><array>
<string>` + bin + `</string><string>watch</string><string>-dir</string><string>` + dir + `</string><string>-email</string><string>` + email + `</string>
</array>
<key>EnvironmentVariables</key><dict>
`)
		for k, v := range env {
			b.WriteString("<key>" + k + "</key><string>" + v + "</string>\n")
		}
		b.WriteString(`</dict>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>` + filepath.Join(home, "Library", "Logs", "cardo-watch.log") + `</string>
<key>StandardErrorPath</key><string>` + filepath.Join(home, "Library", "Logs", "cardo-watch.log") + `</string>
</dict></plist>
`)
		if err := os.WriteFile(plist, []byte(b.String()), 0o644); err != nil {
			return err
		}
		_ = exec.Command("launchctl", "unload", plist).Run()
		if out, err := exec.Command("launchctl", "load", plist).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl load: %v: %s", err, strings.TrimSpace(string(out)))
		}
		fmt.Println("INSTALLED  launchd agent com.cardo.watch ->", plist)
		return nil
	case "linux":
		unitDir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(unitDir, 0o755); err != nil {
			return err
		}
		unit := filepath.Join(unitDir, "cardo-watch.service")
		var b strings.Builder
		b.WriteString("[Unit]\nDescription=Cardo transcript watcher\n\n[Service]\nType=simple\n")
		b.WriteString("ExecStart=" + bin + " watch -dir " + dir + " -email " + email + "\n")
		for k, v := range env {
			b.WriteString("Environment=" + k + "=" + v + "\n")
		}
		b.WriteString("Restart=always\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
		if err := os.WriteFile(unit, []byte(b.String()), 0o644); err != nil {
			return err
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		if out, err := exec.Command("systemctl", "--user", "enable", "--now", "cardo-watch.service").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl: %v: %s", err, strings.TrimSpace(string(out)))
		}
		fmt.Println("INSTALLED  systemd user unit cardo-watch.service")
		return nil
	default:
		return fmt.Errorf("this build cannot install a service on %s; run `cardo watch` from Task Scheduler", runtime.GOOS)
	}
}

func uninstallService() error {
	switch runtime.GOOS {
	case "darwin":
		plist := filepath.Join(homeDir(), "Library", "LaunchAgents", "com.cardo.watch.plist")
		_ = exec.Command("launchctl", "unload", plist).Run()
		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("REMOVED    com.cardo.watch")
		return nil
	case "linux":
		_ = exec.Command("systemctl", "--user", "disable", "--now", "cardo-watch.service").Run()
		_ = os.Remove(filepath.Join(homeDir(), ".config", "systemd", "user", "cardo-watch.service"))
		fmt.Println("REMOVED    cardo-watch.service")
		return nil
	default:
		return fmt.Errorf("nothing to uninstall on %s", runtime.GOOS)
	}
}
