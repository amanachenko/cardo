//go:build darwin

package launcher

import "os/exec"

// OpenBrowser hands the URL to the default browser. It starts and returns; it does not wait for the
// browser to load, and it never raises an error into the installer's exit code.
func OpenBrowser(url string) error {
	return exec.Command("open", url).Start()
}
