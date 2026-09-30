package clickhouse

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/source"
)

// sampleSource keeps invented data in its own partitions, separate from anything real, and makes
// it obvious in every query result which is which.
const sampleSource = source.Name("sample")

// TestSeedSampleData fills ClickHouse with plausible-looking activity so the dashboards have
// something to draw before a real Admin API key exists:
//
//	CARDO_SEED_SAMPLE=1 CARDO_CLICKHOUSE_URL=... go test ./internal/store/clickhouse/ -run TestSeedSampleData
//
// It is env-gated because it writes to a real database and, unlike every other test here, does not
// clean up after itself.
//
// The numbers are invented. That is what the name and the `sample` source are for: no screenshot
// taken from this data should ever be mistaken for a measurement of anything.
func TestSeedSampleData(t *testing.T) {
	if os.Getenv("CARDO_SEED_SAMPLE") == "" {
		t.Skip("set CARDO_SEED_SAMPLE=1 to write sample data into ClickHouse")
	}
	st := testStore(t)

	const (
		days      = 21
		maxActors = 6
	)
	// Fixed seed: re-running the seeder must not redraw the charts.
	rng := rand.New(rand.NewSource(20260923))
	models := []string{"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5-20251001"}

	total := 0
	for d := days; d >= 1; d-- {
		day := time.Now().UTC().AddDate(0, 0, -d).Truncate(24 * time.Hour)

		// Adoption ramps across the window so the fleet panel has a shape rather than a flat
		// line, with the occasional quiet day.
		active := 2 + (maxActors-2)*(days-d)/days
		if rng.Intn(7) == 0 && active > 1 {
			active--
		}

		recs := make([]record, 0, active)
		for a := 0; a < active; a++ {
			accepted := 5 + rng.Intn(40)
			recs = append(recs, record{
				email:    fmt.Sprintf("sample.engineer.%d@example.invalid", a),
				sessions: 1 + rng.Intn(6),
				accepted: accepted,
				rejected: rng.Intn(1 + accepted/4),
				cents:    40 + rng.Intn(600),
				model:    models[a%len(models)],
			})
		}
		putNoCleanup(t, st, sampleSource, day, recs)
		total += len(recs)
	}
	t.Logf("seeded %d sample records across %d days under source=%q", total, days, sampleSource)
}

// TestDropSampleData removes what the seeder wrote.
//
//	CARDO_DROP_SAMPLE=1 CARDO_CLICKHOUSE_URL=... go test ./internal/store/clickhouse/ -run TestDropSampleData
func TestDropSampleData(t *testing.T) {
	if os.Getenv("CARDO_DROP_SAMPLE") == "" {
		t.Skip("set CARDO_DROP_SAMPLE=1 to delete sample data from ClickHouse")
	}
	st := testStore(t)
	for d := 0; d <= 400; d++ {
		key := time.Now().UTC().AddDate(0, 0, -d).Format("2006-01-02")
		partition := fmt.Sprintf("('%s', '%s')", sampleSource, key)
		for _, tbl := range []string{BronzeTable, stagingTable} {
			_ = st.c.Exec(context.Background(), fmt.Sprintf("ALTER TABLE %s DROP PARTITION %s", tbl, partition))
		}
	}
	got := query(t, st, fmt.Sprintf(
		"SELECT count() FROM %s WHERE source = '%s' FORMAT TabSeparated", BronzeTable, sampleSource))
	if got != "0" {
		t.Errorf("%s sample rows remain", got)
	}
}
