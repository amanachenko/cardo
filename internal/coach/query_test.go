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
