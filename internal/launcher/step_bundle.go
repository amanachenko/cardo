package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errNoNetworkAddress is the team path without a CARDO_HOSTNAME: the bundle would point every
// laptop at itself.
var errNoNetworkAddress = errors.New("no network address for the collector")

// errPathUnknown is a store that did not answer which path this stack is on, with no hostname set:
// a loopback bundle would be right for one machine and wrong for a team's.
var errPathUnknown = errors.New("cannot tell which path this stack is on")

// stepBundle writes the managed-settings bundle: the file an organization hands to the machines it
// manages.
//
// The bundle is the entire client side of Cardo. It configures telemetry and nothing else, and
// every hook in it waits for the collector at most a second (ADR-0024, ADR-0039). The launcher
// writes the file and says where it is; a human hands it to the organization's MDM. Installing it
// on real developer machines is the operator's decision, not the launcher's.
//
// The address in the file is the collector's. On a laptop the collector is on loopback, and the
// file points there. A stack that serves a team reaches the collector through the edge proxy, and
// then the address is the hostname the certificate is for, with the two ports the edge publishes:
// 4318 for OTLP and 8088 for the hook pack. The hook URL carries the same host as the OTLP one,
// because test/bundle_test.go holds that they agree.
func stepBundle(opts Options) (Result, error) {
	b, err := bundleFor(opts)
	if err != nil {
		if errors.Is(err, errNoNetworkAddress) {
			return Result{
				ID: "bundle", Name: "BUNDLE", Value: errNoNetworkAddress.Error(),
				Cause: "This stack is on the team path, and without a hostname the bundle would point every machine at itself: on each laptop 127.0.0.1 is the laptop. Nothing would ever arrive, and nothing would say why.",
				Fix:   "set CARDO_HOSTNAME in .env, the name the certificate is for",
			}, nil
		}
		if errors.Is(err, errPathUnknown) {
			return Result{
				ID: "bundle", Name: "BUNDLE", Value: errPathUnknown.Error(),
				Cause: "The store did not answer whether this stack serves a team, and no hostname is set. On the team path a loopback bundle would point every machine at itself.",
				Fix:   "start the stack with cardo up, or set CARDO_HOSTNAME in .env, the name the certificate is for",
			}, nil
		}
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
// CARDO_HOSTNAME, the name the certificate is for, decides the address whenever it is set — a solo
// stack behind the network overlay needs it too. Without it the collector is on loopback and the
// bundle points there, which is the same address the single-engineer install uses. On the team
// path that is a failure, not a loopback bundle: on each engineer's laptop that address is the
// laptop itself. A store that could not say which path this is fails too, rather than guess.
func bundleFor(opts Options) ([]byte, error) {
	env, err := ReadDotEnv(filepath.Join(opts.Dir, ".env"))
	if err != nil {
		env = map[string]string{}
	}
	host := strings.TrimSpace(env["CARDO_HOSTNAME"])
	if host == "" {
		if opts.Pilot {
			return nil, errNoNetworkAddress
		}
		if opts.pathUnknown {
			return nil, errPathUnknown
		}
		return SettingsBundle()
	}
	return SettingsBundleAt(host, "https", "4318", "8088")
}
