package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// SettingsTable holds the deployment choices the collector-path views read when they run
// (sql/clickhouse/005_settings.sql).
const SettingsTable = Database + ".settings"

// MinGroupSizeFloor is the smallest group a view will name: a home-grown artifact, or a cohort
// (ADR-0029). It is a floor, not a default. An operator may raise the size and never lower it,
// and the settings_effective view applies the same floor, so a row written by hand cannot lower
// it either.
const MinGroupSizeFloor = 5

// ViewSettings are the values `cardo migrate` writes for the views. A nil field is left as it
// is. A field pointing at the empty string resets that setting to its default: no organization
// artifacts, and the floor as the group size. The last write wins.
type ViewSettings struct {
	OrgArtifacts *string
	MinGroupSize *string
}

// Validate refuses a value the views must never be given. It runs before anything is written.
func (vs ViewSettings) Validate() error {
	if vs.OrgArtifacts != nil && *vs.OrgArtifacts != "" {
		// The views match with ClickHouse's RE2 and the collector with Go's, which accept the
		// same syntax. A pattern Go cannot compile would fail every view that reads it.
		if _, err := regexp.Compile(*vs.OrgArtifacts); err != nil {
			return fmt.Errorf("CARDO_ORG_ARTIFACTS is not a valid regular expression: %w", err)
		}
	}
	if vs.MinGroupSize != nil && *vs.MinGroupSize != "" {
		n, err := strconv.ParseUint(strings.TrimSpace(*vs.MinGroupSize), 10, 32)
		if err != nil {
			return fmt.Errorf("CARDO_MIN_GROUP_SIZE %q is not a whole number", *vs.MinGroupSize)
		}
		if n < MinGroupSizeFloor {
			return fmt.Errorf(
				"CARDO_MIN_GROUP_SIZE is %d, below the floor of %d.\n"+
					"  The minimum group size can be raised, never lowered (ADR-0029). Below it, a\n"+
					"  command only one engineer uses is a quasi-identifier, and a team chart is one\n"+
					"  person's numbers. Unset the variable to use %d",
				n, MinGroupSizeFloor, MinGroupSizeFloor)
		}
	}
	return nil
}

// ApplyViewSettings validates and writes the settings that were given.
func (s *Store) ApplyViewSettings(ctx context.Context, vs ViewSettings) error {
	if err := vs.Validate(); err != nil {
		return err
	}
	for _, kv := range []struct {
		key   string
		value *string
	}{
		{"org_artifacts", vs.OrgArtifacts},
		{"min_group_size", vs.MinGroupSize},
	} {
		if kv.value == nil {
			continue
		}
		params := url.Values{
			"param_key":    {kv.key},
			"param_value":  {strings.TrimSpace(*kv.value)},
			"async_insert": {"0"},
		}
		if _, err := s.c.do(ctx, params, strings.NewReader(insertSetting)); err != nil {
			return fmt.Errorf("writing view setting %s: %w", kv.key, err)
		}
	}
	return nil
}

// insertSetting appends one setting at the next version. The server computes the version from the
// table in the same statement, so no clock decides which write is the newest
// (sql/clickhouse/005_settings.sql says why). The value travels as a query parameter and is never
// spliced into the SQL: a regex is exactly the kind of value that holds quotes and backslashes.
const insertSetting = "INSERT INTO " + SettingsTable + " (key, value, version) " +
	"SELECT {key:String}, {value:String}, (SELECT max(version) + 1 FROM " + SettingsTable + ")"

// EffectiveViewSettings reads the values the views are using now, after defaults and the floor.
func (s *Store) EffectiveViewSettings(ctx context.Context) (orgArtifacts string, minGroupSize int, err error) {
	out, err := s.c.Query(ctx,
		"SELECT org_artifacts, min_group_size FROM "+Database+".settings_effective FORMAT JSONEachRow")
	if err != nil {
		return "", 0, fmt.Errorf("reading view settings: %w", err)
	}
	var row struct {
		OrgArtifacts string `json:"org_artifacts"`
		MinGroupSize int    `json:"min_group_size"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &row); err != nil {
		return "", 0, fmt.Errorf("decoding view settings %q: %w", out, err)
	}
	return row.OrgArtifacts, row.MinGroupSize, nil
}
