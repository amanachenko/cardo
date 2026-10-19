//go:build !darwin && !linux && !windows

package launcher

import "fmt"

// OpenBrowser is not implemented here; the URL is still printed by the caller.
func OpenBrowser(url string) error {
	return fmt.Errorf("opening a browser is not supported on this platform; open %s manually", url)
}
