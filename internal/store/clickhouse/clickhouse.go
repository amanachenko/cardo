package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
)

// BronzeTable and stagingTable are the two halves of the atomic day swap described in
// sql/clickhouse/001_bronze.sql.
const (
	BronzeTable  = Database + ".bronze_actor_day"
	stagingTable = Database + ".bronze_actor_day_staging"
)

// Store is the ADR-0009 reference storage target.
type Store struct {
	c   *Client
	log *slog.Logger
}

// New returns a Store. It does not connect; Migrate is the first call that touches the server.
func New(cfg Config, log *slog.Logger) (*Store, error) {
	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Store{c: c, log: log}, nil
}

// Host returns the configured destination, for logging.
func (s *Store) Host() string { return s.c.Host() }

// IsPrivateDestination reports whether the destination is an obviously-internal address.
func (s *Store) IsPrivateDestination() bool { return s.c.IsPrivateDestination() }

// Migrate applies the embedded schema. Safe to call on every run.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.c.Ping(ctx); err != nil {
		return fmt.Errorf("connecting to ClickHouse: %w", err)
	}
	return Migrate(ctx, s.c, s.log)
}

// row is the bronze record as written to ClickHouse.
//
// The column set is identical to the JSONL store's row (internal/store/jsonl/jsonl.go) and to
// sql/clickhouse/001_bronze.sql. One list, three places, checked by the INV-5 allowlist test.
//
// Payload is a string rather than json.RawMessage on purpose: the ClickHouse column is String,
// holding the verbatim payload as text (ADR-0010). Marshalling a RawMessage would emit a JSON
// object where the column expects a quoted string, and the insert would be rejected.
type row struct {
	Source       source.Name `json:"source"`
	Day          string      `json:"day"`
	Pseudonym    string      `json:"pseudonym"`
	SaltVersion  string      `json:"salt_version"`
	Tier         int         `json:"tier"`
	ActorType    string      `json:"actor_type"`
	OrgID        string      `json:"org_id"`
	CustomerType string      `json:"customer_type"`
	TerminalType string      `json:"terminal_type"`
	Payload      string      `json:"payload"`
	Unknown      []string    `json:"unknown_fields"`
}

var (
	dayPattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	sourcePattern = regexp.MustCompile(`^[a-z_]+$`)
)

// Put replaces everything held for one source and one day.
//
// The swap is atomic. Rows go into a staging table and are moved across with REPLACE PARTITION,
// so a reader sees either the previous version of the day or the new one. Writing straight into
// bronze would mean deleting the day first, and a process that died in between would leave the
// day silently empty -- which on a dashboard is indistinguishable from a day nobody worked.
func (s *Store) Put(ctx context.Context, src source.Name, day string, records []pseudonym.Scrubbed) error {
	// These two values are interpolated into DDL, so they are checked rather than trusted. Both
	// are internally generated today; that is exactly the assumption worth not relying on.
	if !dayPattern.MatchString(day) {
		return fmt.Errorf("refusing to write partition for malformed day %q", day)
	}
	if !sourcePattern.MatchString(string(src)) {
		return fmt.Errorf("refusing to write partition for malformed source %q", src)
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return fmt.Errorf("refusing to write partition for invalid day %q: %w", day, err)
	}
	partition := fmt.Sprintf("('%s', '%s')", src, day)

	if len(records) == 0 {
		// A day with no activity is a real answer, and it must overwrite whatever was there
		// before -- otherwise a corrected day that lost all its rows keeps reporting the old ones.
		return s.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", BronzeTable, partition))
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, rec := range records {
		// The partition literal above is built from src, while the row's own source column is
		// what ClickHouse actually hashes into a partition. If those disagree, the REPLACE
		// PARTITION below swaps a partition that does not contain these rows -- the write lands
		// somewhere the next poll will never overwrite, and the day quietly double-counts.
		if rec.Source() != src {
			return fmt.Errorf(
				"record for %s carries source %q but is being stored under %q; "+
					"these must match or the partition swap targets the wrong partition",
				day, rec.Source(), src)
		}
		unknown := rec.Unknown()
		if unknown == nil {
			unknown = []string{}
		}
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
			Payload:      string(rec.Raw()),
			Unknown:      unknown,
		}); err != nil {
			return fmt.Errorf("encoding record for %s: %w", day, err)
		}
	}

	if err := s.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", stagingTable, partition)); err != nil {
		return err
	}
	if err := s.c.Insert(ctx, stagingTable, buf.Bytes()); err != nil {
		return fmt.Errorf("staging %d records for %s: %w", len(records), day, err)
	}
	if err := s.exec(ctx, fmt.Sprintf("ALTER TABLE %s REPLACE PARTITION %s FROM %s",
		BronzeTable, partition, stagingTable)); err != nil {
		return err
	}
	// Best effort. A leftover staging partition is harmless -- the next write for this day drops
	// it first -- so a failure here must not fail a poll whose data is already committed.
	if err := s.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", stagingTable, partition)); err != nil {
		s.log.Debug("could not clean staging partition", "day", day, "err", err)
	}
	return nil
}

func (s *Store) exec(ctx context.Context, statement string) error {
	if err := s.c.Exec(ctx, statement); err != nil {
		return fmt.Errorf("%s: %w", firstLine(statement), err)
	}
	return nil
}

// Close releases the connection pool.
func (s *Store) Close() error { return s.c.Close() }
