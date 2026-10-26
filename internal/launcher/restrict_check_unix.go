//go:build !windows

package launcher

import "os"

// restrictedToOwner reports whether only the owner can read the file, and why not when it cannot.
// On Unix that is the mode: 0600 and nothing wider.
func restrictedToOwner(path string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		return false, "mode is " + octal(perm) + ", want 600"
	}
	return true, ""
}

// octal renders a permission mask the way a person reads it.
func octal(perm os.FileMode) string {
	const digits = "01234567"
	out := []byte("0000")
	for i := 3; i >= 0; i-- {
		out[i] = digits[perm&7]
		perm >>= 3
	}
	return string(out)
}
