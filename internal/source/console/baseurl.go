package console

import (
	"fmt"
	"net/url"
	"strings"
)

// LoopbackBase accepts a base URL only when it points at this machine. The endpoint is pinned in
// code so an Admin API key cannot be redirected to a host someone else controls; a local stand-in
// (mockadmin) is the one exception, and it is verified here rather than trusted from configuration.
func LoopbackBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("base URL is not a URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("base URL must be http or https, got %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return strings.TrimRight(u.String(), "/"), nil
	}
	return "", fmt.Errorf("base URL host %q is not loopback: the Admin API endpoint is pinned so the key cannot be sent elsewhere", host)
}
