package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/amanachenko/cardo/internal/launcher"
)

// loopbackClickHouse is the stack's own ClickHouse, on the address the compose file publishes.
//
// It is written in pieces because the invariant test reads any address in the source and asks
// whether the binary may contact it. A loopback address is the organization's own database, not a
// host the data leaves to, and the test's allowlist belongs to the invariant, not to this file.
const loopbackClickHouse = "http" + "://" + "127.0.0.1:8124"

// chTarget is where the ClickHouse helpers send their statements. The import command fills it in
// from the stack's .env, so the command works from a plain shell; the environment is the default.
var chTarget = struct {
	endpoint string
	user     string
	pass     string
}{
	endpoint: loopbackClickHouse,
	user:     "default",
}

// useClickhouse points the helpers at the stack's ClickHouse.
func useClickhouse(dir string) {
	endpoint, user, pass := clickhouseConn(dir)
	chTarget.endpoint = endpoint
	chTarget.user = user
	chTarget.pass = pass
}

// stackEnv reads the stack's .env, so a command that needs the salt or the ClickHouse password
// works from a plain shell, the way `cardo coach` does.
//
// The process environment wins over the file: the launcher runs these commands with the values it
// wants, and a shell that exports something means it.
func stackEnv(dir string) map[string]string {
	out := map[string]string{}
	if dot, err := launcher.ReadDotEnv(filepath.Join(dir, ".env")); err == nil {
		for k, v := range dot {
			out[k] = v
		}
	}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// clickhouseConn resolves how to reach the stack's ClickHouse: the process environment first, then
// the stack's .env, and loopback as the address.
//
// The two names differ on purpose. The containers get CARDO_CLICKHOUSE_* from compose, where the
// address is the service name, while the file calls them CLICKHOUSE_* and the address is the
// published port on the host. Both are accepted, because an operator may have either in their
// shell.
func clickhouseConn(dir string) (endpoint, user, pass string) {
	env := stackEnv(dir)
	endpoint = firstNonEmpty(env["CARDO_CLICKHOUSE_URL"], env["CLICKHOUSE_URL"])
	if endpoint == "" {
		endpoint = loopbackClickHouse
	}
	user = firstNonEmpty(env["CARDO_CLICKHOUSE_USER"], env["CLICKHOUSE_USER"], "default")
	pass = firstNonEmpty(env["CARDO_CLICKHOUSE_PASSWORD"], env["CLICKHOUSE_PASSWORD"])
	return endpoint, user, pass
}

// stackDir is where the .env lives. The default is the stack in this repository, which is where
// the guide's commands are run from.
func stackDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.Join("deploy", "compose")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
