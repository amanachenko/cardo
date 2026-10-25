package launcher

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/directory"
	"github.com/amanachenko/cardo/internal/pseudonym"
)

// TeamsWrite is a hashed team directory, ready to be stored.
//
// The rows are already hashed: an address never reaches this point, and the file it came from is
// never stored either (ADR-0046). The write is what the console does when an operator applies a
// directory, and what `cardo teams --apply` does; both go through here so the two cannot differ.
type TeamsWrite struct {
	Endpoint    string
	User        string
	Password    string
	SaltVersion string
	// Rows are (pseudonym, team) pairs.
	Rows [][2]string
	// SourceHash is the SHA-256 of the uploaded file. It proves what was loaded without containing
	// an address.
	SourceHash string
	// Teams is how many teams the file named, for the batch marker.
	Teams int
}

// TeamsResult says what a write did, so a caller can audit it.
type TeamsResult struct {
	// Batch is the name of the new batch. It is what makes the mapping visible.
	Batch string
	// Rows is how many people were written.
	Rows int
}

// WriteTeams stores a hashed team directory as a new batch.
//
// Rows go in first and the batch marker last: a crash between the two must not publish a partial
// mapping as the current one, because the marker is what makes a batch visible. The previous
// generation is kept, so a mistaken upload is undone by loading the file again.
func WriteTeams(ctx context.Context, w TeamsWrite) (TeamsResult, error) {
	var res TeamsResult
	if len(w.Rows) == 0 {
		return res, fmt.Errorf("the directory has no rows")
	}
	if w.SaltVersion == "" {
		w.SaltVersion = "v1"
	}

	rnd := make([]byte, 4)
	if _, err := rand.Read(rnd); err != nil {
		return res, fmt.Errorf("random bytes are unavailable: %w", err)
	}
	batch := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(rnd)
	res = TeamsResult{Batch: batch, Rows: len(w.Rows)}

	for start := 0; start < len(w.Rows); start += 500 {
		end := start + 500
		if end > len(w.Rows) {
			end = len(w.Rows)
		}
		var b strings.Builder
		b.WriteString("INSERT INTO cardo.settings_teams (team, pseudonym, salt_version, batch) VALUES ")
		for i, row := range w.Rows[start:end] {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "('%s','%s','%s','%s')", escapeSQL(row[1]), escapeSQL(row[0]), escapeSQL(w.SaltVersion), batch)
		}
		if _, _, err := chQuery(ctx, w.Endpoint, w.User, w.Password, b.String()); err != nil {
			return res, fmt.Errorf("the mapping could not be written: %w", err)
		}
	}
	if _, _, err := chQuery(ctx, w.Endpoint, w.User, w.Password, fmt.Sprintf(
		"INSERT INTO cardo.settings_teams_batches (batch, source_hash, rows, teams) VALUES ('%s','%s',%d,%d)",
		batch, w.SourceHash, len(w.Rows), w.Teams)); err != nil {
		return res, fmt.Errorf("the batch marker could not be written, so the mapping is not published: %w", err)
	}

	// Keep the current and the previous generation; anything older is pruned so a weekly reload
	// does not grow a directory forever.
	if body, _, err := chQuery(ctx, w.Endpoint, w.User, w.Password,
		"SELECT batch FROM cardo.settings_teams_batches ORDER BY loaded_at DESC LIMIT 2 FORMAT TSV"); err == nil {
		var ids []string
		for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
			if id := strings.TrimSpace(line); id != "" {
				ids = append(ids, "'"+escapeSQL(id)+"'")
			}
		}
		if len(ids) == 2 {
			list := strings.Join(ids, ", ")
			_, _, _ = chQuery(ctx, w.Endpoint, w.User, w.Password,
				"ALTER TABLE cardo.settings_teams DELETE WHERE batch NOT IN ("+list+") SETTINGS mutations_sync = 1")
			_, _, _ = chQuery(ctx, w.Endpoint, w.User, w.Password,
				"ALTER TABLE cardo.settings_teams_batches DELETE WHERE batch NOT IN ("+list+") SETTINGS mutations_sync = 1")
		}
	}
	return res, nil
}

// HashDirectory turns a parsed directory into the (pseudonym, team) rows to store, using the
// stack's salt. The addresses are hashed here and go no further (ADR-0046).
func HashDirectory(entries []directory.Entry, salt, version string) ([][2]string, error) {
	if strings.TrimSpace(salt) == "" {
		return nil, fmt.Errorf("CARDO_SALT is not set in .env; the directory cannot be hashed")
	}
	if version == "" {
		version = "v1"
	}
	hasher, err := pseudonym.New(salt, version)
	if err != nil {
		return nil, fmt.Errorf("the salt is not usable: %w", err)
	}
	hashed, err := directory.Hash(entries, hasher)
	if err != nil {
		return nil, err
	}
	rows := make([][2]string, 0, len(hashed))
	for _, r := range hashed {
		rows = append(rows, [2]string{r.Pseudonym, r.Team})
	}
	return rows, nil
}

// SourceHashOf is the SHA-256 of a directory file, for the batch marker.
func SourceHashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
