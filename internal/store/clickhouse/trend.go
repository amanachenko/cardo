package clickhouse

// The trend rows that outlive their rows (ADR-0055).
//
// ADR-0016 keeps derived aggregates past the window so a trend survives the expiry of the rows
// behind it. These tables are that promise: no pseudonym, no content, every row describing at
// least min_group_size people, and no TTL at all. `cardo trend` is what writes them, and it has to
// run while the rows it reads are still alive.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/retention"
)

// trendTable is one materialized period: where the rows live, where they are staged, what computes
// them, and which period this call replaces.
type trendTable struct {
	table     string
	staging   string
	source    string
	key       string // the period column: day or week
	period    string // the period being replaced, YYYY-MM-DD
	partition string
}

func (t trendTable) where() string {
	return fmt.Sprintf("%s = '%s'", t.key, t.period)
}

// Trend recomputes the trend rows for one day and swaps them into place.
//
// The day's two tables and the week's artifact table are refreshed together: a day is where events
// land and a week is what adoption is counted over. Each swap is its own REPLACE PARTITION through
// a staging table, so a reader sees either the previous version of the period or the new one --
// never a half-written one. Writing straight into the table would mean deleting the period first,
// and a process that died in between would leave it silently empty, which on a dashboard is
// indistinguishable from a period nobody worked.
//
// A period whose rows have expired is refused. Recomputing it would replace a complete aggregate
// with an empty one and quietly shrink history, which is the same silent failure these rows exist
// to prevent. A retention window of zero means nothing expires, so any period may be recomputed.
func (s *Store) Trend(ctx context.Context, day string) error {
	// Both of these are interpolated into DDL, so they are checked rather than trusted.
	// dayPattern is the same check Put makes.
	if !dayPattern.MatchString(day) {
		return fmt.Errorf("refusing to recompute trend rows for malformed day %q", day)
	}
	// Parsed in the local zone so "the window in force" and the day being recomputed are compared
	// on the same calendar, not on UTC's.
	d, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return fmt.Errorf("refusing to recompute trend rows for invalid day %q: %w", day, err)
	}

	window, err := s.retentionDays(ctx)
	if err != nil {
		return err
	}

	targets := make([]trendTable, 0, 3)
	if s.periodIsRetained(window, d) {
		targets = append(targets,
			trendTable{
				table: trendDayTable, staging: trendDayTable + "_staging",
				source: trendDaySource, key: "day", period: day,
				partition: fmt.Sprintf("tuple('%s')", day),
			},
			trendTable{
				table: trendDayCohortTable, staging: trendDayCohortTable + "_staging",
				source: trendDayCohortSource, key: "day", period: day,
				partition: fmt.Sprintf("tuple('%s')", day),
			})
	} else {
		return fmt.Errorf(
			"refusing to recompute %s: its rows are older than the %d-day retention window and have "+
				"expired. Recomputing it would replace a complete trend row with an empty one, which on "+
				"a dashboard is indistinguishable from a quiet day.", day, window)
	}

	// Adoption is counted over a week, so the whole week is one period. It may be replaced only
	// while every day in it is still retained: the artifact rows of an early day would otherwise be
	// recomputed away by a later day's run.
	offset := (int(d.Weekday()) + 6) % 7 // days since Monday
	week := d.AddDate(0, 0, -offset)
	weekStr := week.Format("2006-01-02")
	if s.periodIsRetained(window, week) {
		targets = append(targets, trendTable{
			table: trendWeekArtifactTable, staging: trendWeekArtifactTable + "_staging",
			source: trendWeekArtifactSource, key: "week", period: weekStr,
			partition: fmt.Sprintf("tuple('%s')", weekStr),
		})
	} else {
		s.log.Debug("skipping the week's adoption rows: the week is older than the retention window",
			"week", weekStr, "window_days", window)
	}

	for _, t := range targets {
		if err := s.replaceTrendPeriod(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

// periodIsRetained says whether the rows behind a period are still there. window 0 means the
// organization keeps rows for ever (ADR-0053's zero is "nothing expires").
func (s *Store) periodIsRetained(window int, day time.Time) bool {
	if window <= 0 {
		return true
	}
	return !day.Before(time.Now().AddDate(0, 0, -window))
}

// replaceTrendPeriod recomputes one period and swaps it in. The order is the one Put uses: clear
// staging, write staging, swap, then tidy staging best-effort.
func (s *Store) replaceTrendPeriod(ctx context.Context, t trendTable) error {
	if err := s.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", t.staging, t.partition)); err != nil {
		return err
	}
	// The source views apply the floor in force when they run, so a period whose groups have all
	// fallen below it writes nothing at all. That is a real answer, and it has to replace what was
	// there -- a corrected period that lost all its rows must stop reporting the old ones.
	//
	// It is written into staging first, so a computation that dies leaves the committed period
	// alone. Nothing that can fail is allowed between the INSERT and the swap.
	if err := s.exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT * FROM %s WHERE %s",
		t.staging, t.source, t.where())); err != nil {
		return fmt.Errorf("computing %s for %s: %w", t.table, t.period, err)
	}
	staged, err := s.stagedCount(ctx, t.staging)
	if err != nil {
		return err
	}
	if staged == 0 {
		// ClickHouse refuses a REPLACE PARTITION from an empty source, because it would drop the
		// destination's data. Here dropping it is the intent, so it is said out loud rather than
		// allowed through a setting a reader would have to know about.
		return s.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", t.table, t.partition))
	}
	if err := s.exec(ctx, fmt.Sprintf("ALTER TABLE %s REPLACE PARTITION %s FROM %s",
		t.table, t.partition, t.staging)); err != nil {
		return err
	}
	// Best effort. A leftover staging partition is harmless -- the next write for this period
	// drops it first -- so a failure here must not fail a run whose rows are already committed.
	if err := s.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", t.staging, t.partition)); err != nil {
		s.log.Debug("could not clean staging partition", "table", t.staging, "period", t.period, "err", err)
	}
	return nil
}

// stagedCount is how many rows this run computed for one period. The staging partition is cleared
// first, so everything in it belongs to the period being replaced.
func (s *Store) stagedCount(ctx context.Context, staging string) (int, error) {
	body, err := s.c.Query(ctx, fmt.Sprintf("SELECT count() FROM %s", staging))
	if err != nil {
		return 0, fmt.Errorf("counting the rows staged in %s: %w", staging, err)
	}
	v := strings.TrimSpace(string(body))
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("the row count staged in %s came back as %q, which is not a number", staging, v)
	}
	return n, nil
}

// retentionDays is the window in force: the one the operator chose (ADR-0053), or the default.
// It is read here rather than passed in, because a caller that could disagree with the store about
// the window would be a caller that can shrink history by accident.
func (s *Store) retentionDays(ctx context.Context) (int, error) {
	body, err := s.c.Query(ctx, "SELECT argMax(value, version) FROM cardo.settings WHERE key = 'retention_days'")
	if err != nil {
		return 0, fmt.Errorf("reading the retention window: %w", err)
	}
	v := strings.TrimSpace(string(body))
	if v == "" {
		return retention.DefaultDays, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("retention_days is stored as %q, which is not a number", v)
	}
	return n, nil
}

const (
	trendDayTable          = Database + ".trend_day"
	trendDayCohortTable    = Database + ".trend_day_cohort"
	trendWeekArtifactTable = Database + ".trend_week_artifact"

	trendDaySource          = Database + ".trend_day_source"
	trendDayCohortSource    = Database + ".trend_day_cohort_source"
	trendWeekArtifactSource = Database + ".trend_week_artifact_source"
)
