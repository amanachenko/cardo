package clickhouse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	cardosql "github.com/amanachenko/cardo/sql"
)

// bootstrap is applied before any migration and is the one piece of schema the runner owns
// itself: the database the migrations qualify against, and the ledger recording what has run.
var bootstrap = []string{
	`CREATE DATABASE IF NOT EXISTS ` + Database,
	`CREATE TABLE IF NOT EXISTS ` + Database + `.schema_migrations
	 (
	     version    String,
	     checksum   String,
	     applied_at DateTime DEFAULT now()
	 )
	 ENGINE = ReplacingMergeTree(applied_at)
	 ORDER BY version`,
}

// Migrate applies every embedded migration that has not run yet, in lexical order.
//
// It is safe to call on every poll, which is why the poller does exactly that: a schema that only
// updates when someone remembers to run a separate command is a schema that will be out of date
// on the machine that matters.
//
// A migration whose content has changed since it was applied is a hard error rather than a
// re-apply. Editing a shipped migration means two installs that report the same version have
// different schemas, and every subsequent difference in their numbers is unexplainable.
func Migrate(ctx context.Context, c *Client, log *slog.Logger) error {
	for _, stmt := range bootstrap {
		if err := c.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("bootstrapping the %s database: %w", Database, err)
		}
	}

	applied, err := appliedVersions(ctx, c)
	if err != nil {
		return err
	}

	files, err := migrationFiles()
	if err != nil {
		return err
	}

	for _, name := range files {
		body, err := cardosql.ClickHouse.ReadFile("clickhouse/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		checksum := migrationChecksum(body)

		if prior, ok := applied[name]; ok {
			if !checksumMatches(prior, body) {
				return fmt.Errorf(
					"migration %s has changed since it was applied.\n"+
						"  Applied checksum: %s\n"+
						"  On-disk checksum: %s\n"+
						"  Migrations are immutable once shipped, for the same reason ADRs are: two\n"+
						"  installs reporting the same version must have the same schema. Add a new\n"+
						"  numbered file instead of editing this one",
					name, prior[:12], checksum[:12])
			}
			log.Debug("migration already applied", "migration", name)
			continue
		}

		for i, stmt := range splitStatements(string(body)) {
			if err := c.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("applying %s (statement %d): %w", name, i+1, err)
			}
		}
		insert := fmt.Sprintf(
			"INSERT INTO %s.schema_migrations (version, checksum) VALUES ('%s', '%s')",
			Database, name, checksum)
		if err := c.Exec(ctx, insert); err != nil {
			return fmt.Errorf("recording migration %s: %w", name, err)
		}
		log.Info("migration applied", "migration", name)
	}
	return nil
}

// migrationChecksum hashes a migration with its line endings normalized to LF.
//
// The binary embeds whatever the working copy holds, and on Windows git with core.autocrlf
// rewrites a file's line endings whenever it touches it. Hashing the raw bytes gave one migration
// two checksums depending on the checkout, so an install could refuse to start the day git flipped
// a file it had already applied. It was seen, not supposed: one working copy held 003_gold.sql
// with CRLF endings and every other migration with LF.
func migrationChecksum(body []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(sum[:])
}

// checksumMatches reports whether a recorded checksum is this migration's. It accepts the
// normalized checksum, and also the one recorded before normalization, which hashed the raw bytes
// of either an LF or a CRLF checkout. A change to the content matches neither.
func checksumMatches(prior string, body []byte) bool {
	lf := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	for _, b := range [][]byte{lf, crlf} {
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) == prior {
			return true
		}
	}
	return false
}

// migrationFiles lists the embedded migrations in the order they must be applied.
func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(cardosql.ClickHouse, "clickhouse")
	if err != nil {
		return nil, fmt.Errorf("listing embedded migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no migrations were embedded; the binary cannot create its schema")
	}
	// Lexical order is the numeric prefix, which is why the prefix is zero-padded.
	sort.Strings(names)
	return names, nil
}

// appliedVersions reads the ledger. FINAL collapses the ReplacingMergeTree so a version that was
// somehow recorded twice reads as one row rather than two conflicting ones.
func appliedVersions(ctx context.Context, c *Client) (map[string]string, error) {
	out, err := c.Query(ctx, fmt.Sprintf(
		"SELECT version, checksum FROM %s.schema_migrations FINAL FORMAT TabSeparated", Database))
	if err != nil {
		return nil, fmt.Errorf("reading the migration ledger: %w", err)
	}
	applied := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		applied[parts[0]] = parts[1]
	}
	return applied, nil
}

// splitStatements turns a migration file into individual statements.
//
// ClickHouse's HTTP interface executes one statement per request, so the file has to be split.
// Line comments are stripped first: they are the only place a semicolon could appear that is not
// a statement terminator, since none of the DDL here contains string literals.
func splitStatements(body string) []string {
	var sb strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	var out []string
	for _, stmt := range strings.Split(sb.String(), ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}
