//go:build windows

package launcher

import "os/exec"

// OpenBrowser goes through the shell's URL handler. `rundll32 url.dll,FileProtocolHandler` is the
// documented way to do this without a console window and without depending on `cmd /c start`.
func OpenBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
