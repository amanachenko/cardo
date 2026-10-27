//go:build linux

package launcher

import (
	"fmt"
	"os/exec"
)

// OpenBrowser uses xdg-open, which is the desktop-agnostic entry point. On a headless host there is
// no browser to open, and the caller keeps the URL anyway, so a missing xdg-open is a plain error
// the caller may ignore.
func OpenBrowser(url string) error {
	path, err := exec.LookPath("xdg-open")
	if err != nil {
		return fmt.Errorf("xdg-open is not installed; open %s manually", url)
	}
	return exec.Command(path, url).Start()
}
