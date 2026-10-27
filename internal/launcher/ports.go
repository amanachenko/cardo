package launcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// BusyPorts returns the subset of ports on host that something already listens on.
//
// It asks by trying to listen: a port that can be bound is free, one that cannot is taken. That is
// the same question the Docker daemon will ask a moment later, so the answer is worth having early.
func BusyPorts(host string, ports []int) ([]int, error) {
	var busy []int
	for _, p := range ports {
		addr := net.JoinHostPort(host, fmt.Sprint(p))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			busy = append(busy, p)
			continue
		}
		_ = ln.Close()
	}
	return busy, nil
}

// WaitHTTP polls url until it answers with a 2xx, the timeout passes, or ctx is cancelled. It is how
// the launcher waits for ClickHouse without guessing a sleep.
func WaitHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var last error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			last = fmt.Errorf("%s answered %d", url, resp.StatusCode)
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			if last == nil {
				last = fmt.Errorf("%s did not answer within %s", url, timeout)
			}
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// portOwnerCommand is the command a person can run to see who holds a port. It is printed in the
// recovery line, so it must be correct for the platform the installer is running on.
func portOwnerCommand(goos string, port int) string {
	if goos == "windows" {
		return fmt.Sprintf("powershell -NoProfile -Command \"Get-NetTCPConnection -LocalPort %d -State Listen | Select-Object -First 1 | ForEach-Object { $_.OwningProcess }\"", port)
	}
	return fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN", port)
}

// portOwner runs portOwnerCommand and returns the first non-header line, or "" when the tool is
// missing. Diagnostics must never fail the step that is already failing.
func portOwner(goos string, port int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var name string
	var args []string
	if goos == "windows" {
		name = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command",
			fmt.Sprintf("Get-NetTCPConnection -LocalPort %d -State Listen | Select-Object -First 1 | ForEach-Object { \"$($_.OwningProcess)\" }", port)}
	} else {
		name = "lsof"
		args = []string{"-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN"}
	}

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "COMMAND") {
			continue
		}
		return strings.Join(strings.Fields(line), " ")
	}
	return ""
}
