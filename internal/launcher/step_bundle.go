package launcher

import (
	"fmt"
	"os"
	"path/filepath"
)

// stepBundle writes the managed-settings bundle: the file an organization hands to the machines it
// manages.
//
// The bundle is the entire client side of Cardo. It configures telemetry and nothing else, and
// every hook in it waits for the collector at most a second (ADR-0024, ADR-0039). The launcher
// writes the file and says where it is; a human hands it to the organization's MDM. Installing it
// on real developer machines is the operator's decision, not the launcher's.
//
// The address in the file is the collector's. On a laptop the collector is on loopback, and the
// file points there. A pilot that serves a team reaches the collector through the edge proxy, and
// then the address is the hostname the certificate is for, with the two ports the edge publishes:
// 4318 for OTLP and 8088 for the hook pack. The hook URL carries the same host as the OTLP one,
// because test/bundle_test.go holds that they agree.
func stepBundle(opts Options) (Result, error) {
	b, err := bundleFor(opts)
	if err != nil {
		return Result{
			ID: "bundle", Name: "BUNDLE", Value: "cannot build the bundle",
			Cause: "The settings bundle could not be generated: " + err.Error(),
			Fix:   "This is a defect in the binary; report it with the message above.",
		}, nil
	}

	path := filepath.Join(opts.Dir, "managed-settings.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return Result{
			ID: "bundle", Name: "BUNDLE", Value: "cannot write the bundle",
			Cause: fmt.Sprintf("Writing %s failed: %v", path, err),
			Fix:   "Check that the directory is writable and that no other process holds the file.",
		}, nil
	}
	return Result{
		ID: "bundle", Name: "BUNDLE",
		Value: path + " — hand this to your MDM; it is the whole client side",
		OK:    true,
	}, nil
}

// bundleFor builds the bundle for the deployment this stack is.
//
// A pilot sets CARDO_HOSTNAME, the name the certificate is for, and the edge proxy serves the
// collector's two ports on it. Without that setting the collector is on loopback and the bundle
// points there, which is the same address the single-engineer install uses.
func bundleFor(opts Options) ([]byte, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		env = map[string]string{}
	}
	host := ""
	if opts.Pilot {
		host = env["CARDO_HOSTNAME"]
	}
	if host == "" {
		return SettingsBundle()
	}
	return SettingsBundleAt(host, "https", "4318", "8088")
}
