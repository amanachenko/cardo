package launcher

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Secrets are the three values the reference stack cannot start without.
type Secrets struct {
	Salt               string
	ClickHousePassword string
	GrafanaPassword    string
}

// cryptoReader is a named type so GenerateSecrets cannot be handed a predictable reader by accident
// in production code, while tests still can.
type cryptoReader struct{}

func (cryptoReader) Read(p []byte) (int, error) { return rand.Read(p) }

// GenerateSecrets returns three independent hex strings of 64 characters (32 bytes).
// The salt must be hex and at least 32 characters; the collector refuses anything else.
func GenerateSecrets(r io.Reader) (Secrets, error) {
	buf := make([]byte, 32*3)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Secrets{}, fmt.Errorf("read random bytes: %w", err)
	}
	return Secrets{
		Salt:               hex.EncodeToString(buf[0:32]),
		ClickHousePassword: hex.EncodeToString(buf[32:64]),
		GrafanaPassword:    hex.EncodeToString(buf[64:96]),
	}, nil
}

// ReadDotEnv parses KEY=VALUE lines. Comments and blank lines are ignored, surrounding quotes are
// stripped, and an inline comment after an unquoted value is dropped.
func ReadDotEnv(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDotEnv(string(b)), nil
}

// ParseDotEnv is ReadDotEnv without the file.
func ParseDotEnv(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[k] = v
	}
	return out
}

// ValidateEnv returns one line per problem, in a stable order. An empty result means the stack can
// start: a hex salt of at least 32 characters and two passwords that are not the example values.
func ValidateEnv(env map[string]string) []string {
	var problems []string
	salt := env["CARDO_SALT"]
	switch {
	case salt == "":
		problems = append(problems, "CARDO_SALT is not set")
	case len(salt) < 32:
		problems = append(problems, fmt.Sprintf("CARDO_SALT is %d characters, want at least 32", len(salt)))
	case !isHex(salt):
		problems = append(problems, "CARDO_SALT must be hex only")
	}
	for _, k := range []string{"CLICKHOUSE_PASSWORD", "GRAFANA_PASSWORD"} {
		switch v := env[k]; {
		case v == "":
			problems = append(problems, k+" is not set")
		case v == "change-me":
			problems = append(problems, k+" is still the example value")
		}
	}
	if n := env["CARDO_MIN_GROUP_SIZE"]; n != "" {
		if n < "5" && n != "" {
			problems = append(problems, "CARDO_MIN_GROUP_SIZE is below 5, which ADR-0029 forbids")
		}
	}
	return problems
}

// WriteDotEnv writes the file with mode 0600. Values already present and valid are kept, so a second
// run never rotates a salt: rotating it would give one engineer two pseudonyms.
func WriteDotEnv(path string, existing map[string]string, s Secrets) error {
	merged := map[string]string{}
	if onDisk, err := ReadDotEnv(path); err == nil {
		for k, v := range onDisk {
			merged[k] = v
		}
	}
	for k, v := range existing {
		merged[k] = v
	}
	if problems := ValidateEnv(merged); len(problems) > 0 {
		if merged["CARDO_SALT"] == "" || !isHex(merged["CARDO_SALT"]) || len(merged["CARDO_SALT"]) < 32 {
			merged["CARDO_SALT"] = s.Salt
		}
		if v := merged["CLICKHOUSE_PASSWORD"]; v == "" || v == "change-me" || len(v) < 8 {
			merged["CLICKHOUSE_PASSWORD"] = s.ClickHousePassword
		}
		if v := merged["GRAFANA_PASSWORD"]; v == "" || v == "change-me" || len(v) < 8 {
			merged["GRAFANA_PASSWORD"] = s.GrafanaPassword
		}
	}
	merged["CARDO_SALT_VERSION"] = valueOr(merged["CARDO_SALT_VERSION"], "v1")

	var b strings.Builder
	b.WriteString("# Written by `cardo up`. Mode 0600. .env is gitignored.\n")
	b.WriteString("# These credentials protect pseudonymous engineering telemetry.\n")
	b.WriteString("# The salt must stay with whoever owns the pseudonyms: rotating it splits one person\n")
	b.WriteString("# into two. In a real deployment the security team holds it (ADR-0006).\n\n")
	written := map[string]bool{}
	for _, k := range []string{"CLICKHOUSE_USER", "CLICKHOUSE_PASSWORD", "GRAFANA_USER", "GRAFANA_PASSWORD", "CARDO_SALT", "CARDO_SALT_VERSION"} {
		v := merged[k]
		if k == "CLICKHOUSE_USER" {
			v = valueOr(v, "cardo")
		}
		if k == "GRAFANA_USER" {
			v = valueOr(v, "admin")
		}
		if v == "" {
			continue
		}
		fmt.Fprintf(&b, "%s=%s\n", k, v)
		written[k] = true
	}
	var rest []string
	for k := range merged {
		if !written[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	if len(rest) > 0 {
		b.WriteString("\n")
		for _, k := range rest {
			fmt.Fprintf(&b, "%s=%s\n", k, merged[k])
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := restrictFile(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return restrictFile(filepath.Clean(path))
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
