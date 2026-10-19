//go:build !windows

package test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInstallScriptInstallsVerifiedBinary exercises the real install.sh against a local release
// layout: it builds the binary, packs the archive, publishes it with checksums.txt, and installs it
// into a temporary directory. It then tampers with the archive and asserts the installer refuses.
func TestInstallScriptInstallsVerifiedBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and runs the shell installer")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not available")
	}

	root := ".."
	work := t.TempDir()
	dist := filepath.Join(work, "dist")
	installDir := filepath.Join(work, "bin")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(work, "cardo")
	build := exec.Command("go", "build", "-o", binary, "./cmd/cardo")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	osName := "darwin"
	if runtime.GOOS == "linux" {
		osName = "linux"
	}
	arch := runtime.GOARCH
	asset := "cardo_" + osName + "_" + arch + ".tar.gz"
	archive := filepath.Join(dist, asset)

	if out, err := exec.Command("tar", "czf", archive, "-C", work, "cardo").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	sum, err := fileSHA256(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "checksums.txt"), []byte(sum+"  "+asset+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(dist)))
	defer srv.Close()

	run := func() (string, error) {
		cmd := exec.Command("bash", "../install.sh")
		cmd.Env = append(os.Environ(), "CARDO_BASE_URL="+srv.URL, "CARDO_BIN="+installDir)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	installed := filepath.Join(installDir, "cardo")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("the installer reported success but wrote no binary: %v\n%s", err, out)
	}
	if got, err := exec.Command(installed, "version").Output(); err != nil || strings.TrimSpace(string(got)) == "" {
		t.Fatalf("installed binary does not answer `version`: %v %q", err, got)
	}

	// Tampering must stop the install, not warn about it.
	f, err := os.OpenFile(archive, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out, err = run()
	if err == nil {
		t.Fatalf("install.sh accepted a tampered archive\n%s", out)
	}
	if !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("refusal does not name the checksum: %s", out)
	}
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
