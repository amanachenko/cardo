package retention

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A stub ClickHouse records the statements it was sent.
type stub struct {
	statements []string
	// value is what the settings table answers for the chosen window.
	value string
}

func (s *stub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sql := string(body)
		s.statements = append(s.statements, sql)
		switch {
		case strings.Contains(sql, "cardo.settings"):
			_, _ = w.Write([]byte(s.value + "\n"))
		case strings.Contains(sql, "count()"):
			_, _ = w.Write([]byte("7\n"))
		default:
			_, _ = w.Write([]byte(""))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Zero means "keep everything": nothing is deleted, and the TTL comes off so that nothing expires
// on its own either. A TTL left in place would keep deleting at the shipped window whatever the
// operator chose, and the console would be showing a number that is not what happens.
func TestZeroKeepsEverythingAndTakesTheTTLOff(t *testing.T) {
	st := &stub{}
	srv := st.server(t)
	report, err := Prune(context.Background(), srv.URL, "u", "p", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range st.statements {
		if strings.Contains(sql, "DELETE WHERE") {
			t.Errorf("a row was deleted while keeping everything: %s", sql)
		}
	}
	removed := 0
	for _, sql := range st.statements {
		if strings.Contains(sql, "REMOVE TTL") {
			removed++
		}
	}
	// Four tables carry a TTL. The staging table never had one, so there is nothing to take off.
	withTTL := 0
	for _, tbl := range Tables(0) {
		if tbl.TTL != "" {
			withTTL++
		}
	}
	if removed != withTTL {
		t.Errorf("%d tables had their TTL taken off, want %d", removed, withTTL)
	}
	for name, n := range report {
		if n != 0 {
			t.Errorf("%s: reported %d deleted rows, want 0", name, n)
		}
	}
}

// A window above zero deletes what is older than it, and moves the TTL with it.
func TestAWindowDeletesAndMovesTheTTL(t *testing.T) {
	st := &stub{}
	srv := st.server(t)
	if _, err := Prune(context.Background(), srv.URL, "u", "p", 30, false); err != nil {
		t.Fatal(err)
	}
	var deleted, moved bool
	for _, sql := range st.statements {
		if strings.Contains(sql, "DELETE WHERE") {
			deleted = true
		}
		if strings.Contains(sql, "MODIFY TTL") && strings.Contains(sql, "toIntervalDay(30)") {
			moved = true
		}
	}
	if !deleted {
		t.Error("nothing was deleted")
	}
	if !moved {
		t.Error("the TTL was not moved to the chosen window")
	}
}

// A dry run counts and changes nothing.
func TestADryRunChangesNothing(t *testing.T) {
	st := &stub{}
	srv := st.server(t)
	report, err := Prune(context.Background(), srv.URL, "u", "p", 30, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range st.statements {
		if strings.Contains(sql, "DELETE WHERE") || strings.Contains(sql, "TTL") {
			t.Errorf("a dry run changed the table: %s", sql)
		}
	}
	if report["cardo.bronze_otel_logs"] != 7 {
		t.Errorf("report = %v, want the counted 7", report)
	}
}

// A negative window is a mistake, not a choice.
func TestANegativeWindowIsRefused(t *testing.T) {
	st := &stub{}
	srv := st.server(t)
	if _, err := Prune(context.Background(), srv.URL, "u", "p", -1, false); err == nil {
		t.Fatal("a negative window was accepted")
	}
}

// The staging table carries no TTL on purpose: its rows live for seconds, between the write and the
// partition swap. Giving it one would contradict the schema, and a TTL there deletes nothing that
// was going to be read anyway.
func TestTheStagingTableIsGivenNoTTL(t *testing.T) {
	for _, tbl := range Tables(30) {
		if tbl.Name == "cardo.bronze_actor_day_staging" && tbl.TTL != "" {
			t.Errorf("the staging table would be given a TTL on %q", tbl.TTL)
		}
	}
	st := &stub{}
	srv := st.server(t)
	if _, err := Prune(context.Background(), srv.URL, "u", "p", 30, false); err != nil {
		t.Fatal(err)
	}
	for _, sql := range st.statements {
		if strings.Contains(sql, "bronze_actor_day_staging") && strings.Contains(sql, "TTL") {
			t.Errorf("the staging table was given a TTL: %s", sql)
		}
	}
}

// The window the operator chose in the console is the one in force: the console writes it to the
// settings table, and that is what the store reads back.
func TestTheChosenWindowComesFromTheSettingsTable(t *testing.T) {
	st := &stub{}
	st.value = "30"
	srv := st.server(t)
	got, err := EffectiveWindow(context.Background(), srv.URL, "u", "p", DefaultDays)
	if err != nil {
		t.Fatal(err)
	}
	if got != 30 {
		t.Errorf("window = %d, want the chosen 30", got)
	}
}

// Nothing chosen means the fallback, which is the default.
func TestNoChosenWindowFallsBack(t *testing.T) {
	st := &stub{}
	st.value = ""
	srv := st.server(t)
	got, err := EffectiveWindow(context.Background(), srv.URL, "u", "p", DefaultDays)
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultDays {
		t.Errorf("window = %d, want the default %d", got, DefaultDays)
	}
}

// The console moves the TTL when the operator saves a window, so the number it shows is the one
// the tables keep, rather than one that waits for the next poll.
func TestApplyWindowMovesTheTTLWithoutDeleting(t *testing.T) {
	st := &stub{}
	srv := st.server(t)
	if err := ApplyWindow(context.Background(), srv.URL, "u", "p", 30); err != nil {
		t.Fatal(err)
	}
	moved := false
	for _, sql := range st.statements {
		if strings.Contains(sql, "MODIFY TTL") && strings.Contains(sql, "toIntervalDay(30)") {
			moved = true
		}
		if strings.Contains(sql, "DELETE WHERE") {
			t.Errorf("applying a window deleted rows: %s", sql)
		}
	}
	if !moved {
		t.Error("the TTL was not moved")
	}
}

// Zero means "keep everything": the TTL comes off, so nothing expires on its own either.
func TestApplyWindowOfZeroTakesTheTTLOff(t *testing.T) {
	st := &stub{}
	srv := st.server(t)
	if err := ApplyWindow(context.Background(), srv.URL, "u", "p", 0); err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, sql := range st.statements {
		if strings.Contains(sql, "REMOVE TTL") {
			removed++
		}
	}
	if removed != 4 {
		t.Errorf("%d tables had their TTL taken off, want 4: the staging table never had one", removed)
	}
}
