package coach

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// coachEnv answers the coach's three queries. The cohort answer is what tells an imported window
// from a live one, so the fake replies with a chosen number of live sessions.
type coachEnv struct {
	liveSessions int
	queries      []string
}

func (e *coachEnv) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		sql := string(body)
		e.queries = append(e.queries, sql)
		switch {
		case strings.Contains(sql, "countIf(cohort != 'imported')"):
			_, _ = w.Write([]byte(itoa(e.liveSessions) + "\n"))
		case strings.Contains(sql, "countIf(ran = 1 AND succeeded = 0)"):
			// Imported history has no tool decisions, so the store holds no rows for it and the
			// query answers zero. A live window answers the figures.
			if e.liveSessions == 0 {
				_, _ = w.Write([]byte("0	0	0\n"))
			} else {
				_, _ = w.Write([]byte("2	5	3\n"))
			}
		case strings.Contains(sql, "uniqExact(session_id)"):
			_, _ = w.Write([]byte("4\t9\t1.50\t1200\t48000\n"))
		default:
			_, _ = w.Write([]byte(""))
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func coachWindow(t *testing.T, liveSessions int) Window {
	t.Helper()
	e := &coachEnv{liveSessions: liveSessions}
	srv := httptest.NewServer(e.handler())
	t.Cleanup(srv.Close)
	to := time.Now().UTC()
	w, _, err := Query(context.Background(), srv.URL, "u", "p", "pseudo", to.AddDate(0, 0, -27), to)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A window of imported history carries no tool decisions, so the figures were never looked for.
// Saying "0 failed" there would claim a measurement nobody made (ADR-0050).
func TestImportedHistoryIsNotObserved(t *testing.T) {
	w := coachWindow(t, 0)
	if w.ToolsObserved {
		t.Error("a window with no live sessions reports its tool figures as observed")
	}
	if w.FailedTools != 0 || w.ToolCalls != 0 {
		t.Errorf("the tool figures are %d/%d, want the zeros the store holds", w.FailedTools, w.ToolCalls)
	}
}

// A window with live telemetry was looked for, and its figures stand.
func TestLiveHistoryIsObserved(t *testing.T) {
	w := coachWindow(t, 4)
	if !w.ToolsObserved {
		t.Error("a window with live sessions does not report its tool figures as observed")
	}
	if w.FailedTools != 2 || w.ToolCalls != 5 {
		t.Errorf("the tool figures are %d/%d, want 2/5", w.FailedTools, w.ToolCalls)
	}
}

// The coach must not carry a field it never fills. A number that looks like a measurement and is
// not one is worse than no number.
func TestTheCoachCarriesNoFieldItNeverFills(t *testing.T) {
	w := coachWindow(t, 4)
	// The window's fields are read by the console and the CLI. Every one of them is filled by the
	// query above, and there is no field that stays at zero because nothing writes it.
	if w.Sessions != 4 || w.Prompts != 9 {
		t.Errorf("the window reads %d sessions and %d prompts, want 4 and 9", w.Sessions, w.Prompts)
	}
	if w.CostUSD != 1.50 {
		t.Errorf("the window reads a cost of %v, want 1.50", w.CostUSD)
	}
}
