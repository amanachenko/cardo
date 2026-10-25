package coach

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQueryParsesTheThreeTables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		switch {
		case strings.Contains(body, "silver_turn"):
			w.Write([]byte("3\t42\t12.50\t9000\t120000\n"))
		case strings.Contains(body, "silver_tool_call"):
			w.Write([]byte("4\t30\t12\n"))
		case strings.Contains(body, "silver_artifact_load"):
			w.Write([]byte("review-pr\t5\n"))
		default:
			w.Write([]byte("\n"))
		}
	}))
	defer srv.Close()

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 27)
	w, arts, err := Query(context.Background(), srv.URL, "u", "p", "abc", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if w.Sessions != 3 || w.Prompts != 42 || w.CostUSD != 12.5 || w.OutputTokens != 9000 || w.PeakContext != 120000 {
		t.Fatalf("turns parsed wrong: %+v", w)
	}
	if w.FailedTools != 4 || w.ToolCalls != 30 || w.Edits != 12 {
		t.Fatalf("tool calls parsed wrong: %+v", w)
	}
	if len(arts) != 1 || arts[0].Name != "review-pr" || arts[0].Loads != 5 {
		t.Fatalf("artifacts parsed wrong: %+v", arts)
	}
}

func TestQuerySurfacesClickHouseErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unknown identifier pseudonym", http.StatusBadRequest)
	}))
	defer srv.Close()
	if _, _, err := Query(context.Background(), srv.URL, "u", "p", "abc", time.Now(), time.Now()); err == nil {
		t.Fatal("expected an error")
	}
}

// The coach is about one person, and a service run is not a person (ADR-0036): the figures must
// leave service sessions out, or a CI job would show up as an engineer's sessions.
func TestQueryLeavesServiceSessionsOut(t *testing.T) {
	var sqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		sqls = append(sqls, body)
		switch {
		case strings.Contains(body, "silver_turn"):
			w.Write([]byte("3	42	12.50	9000	120000\n"))
		case strings.Contains(body, "silver_tool_call"):
			w.Write([]byte("4	30	12\n"))
		default:
			w.Write([]byte("\n"))
		}
	}))
	defer srv.Close()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := Query(context.Background(), srv.URL, "u", "p", "abc", from, from.AddDate(0, 0, 27)); err != nil {
		t.Fatal(err)
	}
	if len(sqls) == 0 {
		t.Fatal("no query was sent")
	}
	for _, sql := range sqls {
		if strings.Contains(sql, "silver_turn") || strings.Contains(sql, "silver_tool_call") {
			if !strings.Contains(sql, "actor_kind != 'service'") {
				t.Errorf("the figures query does not exclude service runs: %s", sql)
			}
		}
	}
}

// The floor is the organization's minimum group size, read from the settings, not a literal five
// (ADR-0029). The coach follows whatever the organization set.
func TestUntriedReadsTheFloorFromTheSettings(t *testing.T) {
	var sql string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sql = string(raw)
		w.Write([]byte("review-pr	11\n"))
	}))
	defer srv.Close()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := Untried(context.Background(), srv.URL, "u", "p", "abc", from, from.AddDate(0, 0, 27))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "review-pr" || got[0].People != 11 {
		t.Fatalf("parsed wrong: %+v", got)
	}
	if !strings.Contains(sql, "settings_effective") {
		t.Errorf("the floor is not read from the settings: %s", sql)
	}
	if strings.Contains(sql, ">= 5") {
		t.Errorf("the floor is a literal five: %s", sql)
	}
	if !strings.Contains(sql, "is_builtin = 0") {
		t.Errorf("Claude Code's own built-ins are not excluded: %s", sql)
	}
	if !strings.Contains(sql, "actor_kind != 'service'") {
		t.Errorf("service runs are not excluded: %s", sql)
	}
}
