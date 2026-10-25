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
}

func (s *stub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sql := string(body)
		s.statements = append(s.statements, sql)
		if strings.Contains(sql, "count()") {
			_, _ = w.Write([]byte("7\n"))
			return
		}
		_, _ = w.Write([]byte(""))
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
	if removed != len(Tables(0)) {
		t.Errorf("%d tables had their TTL taken off, want %d", removed, len(Tables(0)))
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
