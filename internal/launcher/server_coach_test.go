package launcher

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCoachTokenIsPerPseudonymAndStable(t *testing.T) {
	a := coachToken("board-token", "p1")
	b := coachToken("board-token", "p2")
	if a == b {
		t.Fatal("two pseudonyms share a token")
	}
	if a != coachToken("board-token", "p1") {
		t.Fatal("token is not stable")
	}
	if a == coachToken("other-board", "p1") {
		t.Fatal("a different board token must invalidate the link")
	}
	if len(a) != 32 {
		t.Fatalf("unexpected token length %d", len(a))
	}
}

func TestCoachOpenRejectsAWrongKeyBeforeReadingData(t *testing.T) {
	s, err := NewServer(Options{Dir: t.TempDir(), Token: "board-token", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/coach/open?pseudonym=p1&k=deadbeef", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key answered %d, want 401", rec.Code)
	}
	if rec.Body.String() == "" {
		t.Fatal("no explanation for the refused link")
	}
}
