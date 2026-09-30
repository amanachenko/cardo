// Package jsonl implements the no-infrastructure storage mode from ADR-0009.
//
// Records are written as newline-delimited JSON under a Hive-style partition layout:
//
//	<root>/bronze/source=console/day=2026-09-20.jsonl
//
// DuckDB reads that layout directly and derives the partition columns from the paths:
//
//	SELECT * FROM read_json_auto('bronze/*/*.jsonl', hive_partitioning = true);
//
// This is the evaluation path a five-engineer pilot can run without asking anyone to approve
// infrastructure, and it needs no database server, no container and no C toolchain.
package jsonl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
)

// Store writes bronze records to partitioned NDJSON files.
type Store struct {
	root string
}

// New returns a Store rooted at dir.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	return &Store{root: dir}, nil
}

// Migrate creates the directory tree. There is no schema to version: the files are raw payloads
// plus the bronze columns, and the canonical shape is imposed by the SQL views that read them
// (ADR-0010).
func (s *Store) Migrate(ctx context.Context) error {
	return os.MkdirAll(filepath.Join(s.root, "bronze"), 0o755)
}

// row is the bronze record as written to disk.
//
// Column set matches docs/design/data-model.md, with the source discriminator required by
// ADR-0021. Note what is absent: there is no email column, and there is no column that could hold
// one.
type row struct {
	Source       source.Name     `json:"source"`
	Day          string          `json:"day"`
	Pseudonym    string          `json:"pseudonym"`
	SaltVersion  string          `json:"salt_version"`
	Tier         int             `json:"tier"`
	ActorType    string          `json:"actor_type"`
	OrgID        string          `json:"org_id"`
	CustomerType string          `json:"customer_type"`
	TerminalType string          `json:"terminal_type"`
	Payload      json.RawMessage `json:"payload"`
	Unknown      []string        `json:"unknown_fields,omitempty"`
}

// Put replaces the file holding one source's records for one day.
//
// The write is atomic: a temporary file is renamed into place, so an interrupted run leaves the
// previous copy intact rather than a half-written file that DuckDB would refuse to parse.
func (s *Store) Put(ctx context.Context, src source.Name, day string, records []pseudonym.Scrubbed) error {
	dir := filepath.Join(s.root, "bronze", fmt.Sprintf("source=%s", src))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating partition %s: %w", dir, err)
	}

	final := filepath.Join(dir, day+".jsonl")
	tmp, err := os.CreateTemp(dir, "."+day+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	enc := json.NewEncoder(tmp)
	for _, rec := range records {
		if err := enc.Encode(row{
			Source:      rec.Source(),
			Day:         day,
			Pseudonym:   rec.Pseudonym(),
			SaltVersion: rec.SaltVersion(),
			// Admin-API aggregates are fleet-core data: pseudonymous, cohort-only, no
			// content. Tier 1 per ADR-0002.
			Tier:         1,
			ActorType:    rec.ActorType(),
			OrgID:        rec.OrgID(),
			CustomerType: rec.CustomerType(),
			TerminalType: rec.TerminalType(),
			Payload:      rec.Raw(),
			Unknown:      rec.Unknown(),
		}); err != nil {
			tmp.Close()
			return fmt.Errorf("encoding record for %s: %w", day, err)
		}
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return fmt.Errorf("publishing %s: %w", final, err)
	}
	return nil
}

// Close is a no-op; files are closed as they are written.
func (s *Store) Close() error { return nil }
