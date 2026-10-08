package poll

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/pseudonym"
	"github.com/amanachenko/cardo/internal/source"
	"github.com/amanachenko/cardo/internal/store/jsonl"
)

const testSalt = "0123456789abcdef0123456789abcdef"

// fakeAdapter returns canned records, so the pipeline can be exercised without a network.
type fakeAdapter struct {
	byDay map[string][]source.Record
	err   error
	calls []string
}

func (f *fakeAdapter) Name() source.Name { return source.Console }

func (f *fakeAdapter) Fetch(ctx context.Context, day time.Time) ([]source.Record, error) {
	key := day.Format("2006-01-02")
	f.calls = append(f.calls, key)
	if f.err != nil {
		return nil, f.err
	}
	return f.byDay[key], nil
}

func recordFor(day, email string) source.Record {
	d, _ := time.Parse("2006-01-02", day)
	raw := fmt.Sprintf(`{"actor":{"type":"user_actor","email_address":%q},"core_metrics":{"num_sessions":3}}`, email)
	return source.Record{
		Source:    source.Console,
		Day:       d,
		ActorType: "user_actor",
		ActorID:   email,
		ActorPath: []string{"actor", "email_address"},
		OrgID:     "org-1",
		Raw:       json.RawMessage(raw),
	}
}

func newRunner(t *testing.T, a source.Adapter) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := jsonl.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h, err := pseudonym.New(testSalt, "v1")
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{
		Adapter: a,
		Hasher:  h,
		Store:   st,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, dir
}

func window(from, to string) source.Window {
	f, _ := time.Parse("2006-01-02", from)
	t2, _ := time.Parse("2006-01-02", to)
	return source.Window{From: f, To: t2}
}

func TestRunStoresEveryDayOldestFirst(t *testing.T) {
	a := &fakeAdapter{byDay: map[string][]source.Record{
		"2026-09-01": {recordFor("2026-09-01", "alice@example.com")},
		"2026-09-02": {recordFor("2026-09-02", "alice@example.com"), recordFor("2026-09-02", "bob@example.com")},
		"2026-09-03": {},
	}}
	r, _ := newRunner(t, a)

	res, err := r.Run(context.Background(), window("2026-09-01", "2026-09-03"))
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(a.calls, ","); got != "2026-09-01,2026-09-02,2026-09-03" {
		t.Errorf("days polled in order %q; oldest first keeps an interrupted backfill contiguous", got)
	}
	if res.Records != 3 {
		t.Errorf("Records = %d, want 3", res.Records)
	}
	if res.DaysPolled != 3 {
		t.Errorf("DaysPolled = %d, want 3", res.DaysPolled)
	}
	if res.DaysWithData != 2 {
		t.Errorf("DaysWithData = %d, want 2", res.DaysWithData)
	}
	if len(res.Actors) != 2 {
		t.Errorf("distinct actors = %d, want 2", len(res.Actors))
	}
}

// The whole point of risks.md #9. An authenticated, well-formed, empty window must not be reported
// as an ordinary success, because a wrong-source misconfiguration looks exactly like a quiet org.
func TestEmptyWindowIsDiagnosedLoudly(t *testing.T) {
	a := &fakeAdapter{byDay: map[string][]source.Record{}}
	r, _ := newRunner(t, a)

	res, err := r.Run(context.Background(), window("2026-09-01", "2026-09-05"))
	if err != nil {
		t.Fatal(err)
	}

	level, msg, _ := res.Diagnose()
	if level != slog.LevelWarn {
		t.Errorf("empty window logged at %v, want WARN", level)
	}
	for _, want := range []string{"Enterprise", "different endpoint", "ADR-0021", "no Claude Code activity"} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnosis should mention %q; got:\n%s", want, msg)
		}
	}
}

// A window whose first day is after its last holds no days. Run used to poll nothing and report
// "poll complete", which reads as a quiet organization when it was a wrong pair of flags.
func TestRunRefusesAWindowWithNoDays(t *testing.T) {
	a := &fakeAdapter{byDay: map[string][]source.Record{}}
	r, _ := newRunner(t, a)

	_, err := r.Run(context.Background(), window("2026-09-05", "2026-09-01"))
	if err == nil {
		t.Fatal("Run() over a window ending before it starts returned no error")
	}
	if len(a.calls) != 0 {
		t.Errorf("Run() fetched %v from a window with no days", a.calls)
	}
}

func TestNonEmptyWindowIsReportedQuietly(t *testing.T) {
	a := &fakeAdapter{byDay: map[string][]source.Record{
		"2026-09-01": {recordFor("2026-09-01", "alice@example.com")},
	}}
	r, _ := newRunner(t, a)

	res, _ := r.Run(context.Background(), window("2026-09-01", "2026-09-01"))
	if level, _, _ := res.Diagnose(); level != slog.LevelInfo {
		t.Errorf("a normal poll logged at %v, want INFO", level)
	}
}

func TestDriftIsSurfacedInTheDiagnosis(t *testing.T) {
	rec := recordFor("2026-09-01", "alice@example.com")
	rec.Unknown = []string{"core_metrics.skills_invoked"}
	a := &fakeAdapter{byDay: map[string][]source.Record{"2026-09-01": {rec}}}
	r, _ := newRunner(t, a)

	res, _ := r.Run(context.Background(), window("2026-09-01", "2026-09-01"))
	level, msg, _ := res.Diagnose()
	if level != slog.LevelWarn {
		t.Errorf("drift logged at %v, want WARN", level)
	}
	if !strings.Contains(msg, "not lost") {
		t.Errorf("drift message should reassure that data is stored verbatim; got: %s", msg)
	}
}

// An identity leak must stop the run. Continuing would write a real email address to disk.
func TestIdentityLeakHaltsTheRun(t *testing.T) {
	leaky := recordFor("2026-09-01", "alice@example.com")
	leaky.Raw = json.RawMessage(`{"actor":{"type":"user_actor","email_address":"alice@example.com"},` +
		`"notes":{"owner":"alice@example.com"}}`)
	a := &fakeAdapter{byDay: map[string][]source.Record{"2026-09-01": {leaky}}}
	r, _ := newRunner(t, a)

	_, err := r.Run(context.Background(), window("2026-09-01", "2026-09-01"))
	if err == nil {
		t.Fatal("expected the run to stop on an identity leak")
	}
	if !strings.Contains(err.Error(), "Stopping the poll") {
		t.Errorf("error should say the run stopped; got: %v", err)
	}
}

// Today is always partial: the Console endpoint excludes data under an hour old. Storing it would
// make the newest day on every dashboard a number that grows all day and is never right.
func TestDefaultWindowEndsYesterday(t *testing.T) {
	now := time.Date(2026, 9, 23, 14, 30, 0, 0, time.UTC)
	w := DefaultWindow(now, 7)

	if got := w.To.Format("2006-01-02"); got != "2026-09-22" {
		t.Errorf("window ends %s, want 2026-09-22 (yesterday)", got)
	}
	if got := w.From.Format("2006-01-02"); got != "2026-09-16" {
		t.Errorf("window starts %s, want 2026-09-16", got)
	}
	if got := len(w.Days()); got != 7 {
		t.Errorf("window covers %d days, want 7", got)
	}
}

func TestWindowDaysAreInclusiveAndOrdered(t *testing.T) {
	days := window("2026-09-01", "2026-09-03").Days()
	if len(days) != 3 {
		t.Fatalf("got %d days, want 3 (both endpoints inclusive)", len(days))
	}
	for i, want := range []string{"2026-09-01", "2026-09-02", "2026-09-03"} {
		if got := days[i].Format("2006-01-02"); got != want {
			t.Errorf("day %d = %s, want %s", i, got, want)
		}
	}
}
