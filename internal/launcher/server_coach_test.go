package launcher

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// The pseudonym reaches SQL, so it is checked before it gets there: 64 lowercase hex characters,
// which is what a salted SHA-256 looks like.
func TestOnlyAPseudonymIsAccepted(t *testing.T) {
	good := strings.Repeat("ab", 32)
	if !validPseudonym(good) {
		t.Errorf("a real pseudonym was refused: %q", good)
	}
	for _, bad := range []string{
		"",
		"p1",
		strings.Repeat("ab", 31),
		strings.Repeat("ab", 33),
		strings.Repeat("AB", 32),           // uppercase
		strings.Repeat("zz", 32),           // not hex
		"'; DROP TABLE cardo.settings; --", // an attempt
	} {
		if validPseudonym(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

// A pseudonym that is not one of ours never reaches a query.
func TestCoachRefusesAPseudonymThatIsNotOurs(t *testing.T) {
	s, err := NewServer(Options{Dir: t.TempDir(), Token: "board-token", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/coach?pseudonym=not-a-pseudonym", nil)
	req.Header.Set("X-Cardo-Token", "board-token")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("answered %d, want 400", rec.Code)
	}
}
