package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/amanachenko/cardo/internal/directory"
)

// teamsEnv stands in for ClickHouse and records what it was asked to store. It answers the two
// questions the write asks: which batches exist, and whether the insert was accepted.
type teamsEnv struct {
	mu      sync.Mutex
	queries []string
	// batches is what the store already holds, newest last.
	batches []string
}

func (e *teamsEnv) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		sql := string(body)
		e.mu.Lock()
		e.queries = append(e.queries, sql)
		batches := append([]string(nil), e.batches...)
		e.mu.Unlock()

		if strings.Contains(sql, "settings_teams_batches") && strings.Contains(sql, "SELECT batch") {
			// The newest batch first: the store answers with the two most recent.
			for i := len(batches) - 1; i >= 0 && i >= len(batches)-2; i-- {
				_, _ = w.Write([]byte(batches[i] + "\n"))
			}
			return
		}
		if strings.Contains(sql, "settings_teams_batches") && strings.Contains(sql, "INSERT") {
			e.mu.Lock()
			e.batches = append(e.batches, "batch-"+itoa(len(e.batches)))
			e.mu.Unlock()
			_, _ = w.Write([]byte(""))
			return
		}
		_, _ = w.Write([]byte(""))
	}
}

func (e *teamsEnv) saw(needle string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, q := range e.queries {
		if strings.Contains(q, needle) {
			return true
		}
	}
	return false
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

// The rows go in before the batch marker. A crash between the two must not publish a partial
// mapping as the current one, because the marker is what makes a batch visible (ADR-0046).
func TestTeamsRowsAreWrittenBeforeTheBatchMarker(t *testing.T) {
	e := &teamsEnv{}
	srv := httptest.NewServer(e.handler())
	t.Cleanup(srv.Close)

	res, err := WriteTeams(context.Background(), TeamsWrite{
		Endpoint:    srv.URL,
		User:        "cardo",
		Password:    "pw",
		SaltVersion: "v1",
		Rows:        [][2]string{{"pseudo-a", "team-alpha"}, {"pseudo-b", "team-alpha"}},
		SourceHash:  "abc123",
		Teams:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 2 {
		t.Errorf("the result says %d rows, want 2", res.Rows)
	}
	if res.Batch == "" {
		t.Error("the result carries no batch name")
	}

	rowsAt, markerAt := -1, -1
	for i, q := range e.queries {
		if strings.Contains(q, "INSERT INTO cardo.settings_teams (") && rowsAt < 0 {
			rowsAt = i
		}
		if strings.Contains(q, "INSERT INTO cardo.settings_teams_batches") && markerAt < 0 {
			markerAt = i
		}
	}
	if rowsAt < 0 {
		t.Fatal("the mapping was never written")
	}
	if markerAt < 0 {
		t.Fatal("the batch marker was never written")
	}
	if rowsAt > markerAt {
		t.Error("the batch marker was written before the rows: a crash in between would publish a partial mapping")
	}
}

// The mapping holds a code name and a team, never an address (ADR-0046).
func TestTeamsStoresNoAddress(t *testing.T) {
	e := &teamsEnv{}
	srv := httptest.NewServer(e.handler())
	t.Cleanup(srv.Close)

	if _, err := WriteTeams(context.Background(), TeamsWrite{
		Endpoint:    srv.URL,
		User:        "cardo",
		Password:    "pw",
		SaltVersion: "v1",
		Rows:        [][2]string{{"deadbeef", "team-alpha"}},
		SourceHash:  "abc123",
		Teams:       1,
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range e.queries {
		if strings.Contains(q, "@") {
			t.Errorf("a statement carries an address: %s", q)
		}
	}
}

// The source hash proves what was loaded without containing an address, so it must be stored.
func TestTeamsStoresTheSourceHash(t *testing.T) {
	e := &teamsEnv{}
	srv := httptest.NewServer(e.handler())
	t.Cleanup(srv.Close)

	if _, err := WriteTeams(context.Background(), TeamsWrite{
		Endpoint:    srv.URL,
		User:        "cardo",
		Password:    "pw",
		SaltVersion: "v1",
		Rows:        [][2]string{{"deadbeef", "team-alpha"}},
		SourceHash:  "thehash",
		Teams:       1,
	}); err != nil {
		t.Fatal(err)
	}
	if !e.saw("thehash") {
		t.Error("the source hash was not stored")
	}
}

// An empty directory is refused: a batch with no rows would publish nothing and hide the fact.
func TestTeamsRefusesAnEmptyDirectory(t *testing.T) {
	e := &teamsEnv{}
	srv := httptest.NewServer(e.handler())
	t.Cleanup(srv.Close)

	if _, err := WriteTeams(context.Background(), TeamsWrite{Endpoint: srv.URL}); err == nil {
		t.Error("an empty directory was accepted")
	}
}

// Hashing turns an address into the same code name the collector produces, and the salt version
// travels with it, or the directory joins nothing (ADR-0046).
func TestHashDirectoryProducesACodeNamePerPerson(t *testing.T) {
	entries := []directory.Entry{
		{Email: "first@example.com", Team: "team-alpha"},
		{Email: "second@example.com", Team: "team-beta"},
	}
	rows, err := HashDirectory(entries, strings.Repeat("ab", 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0][0] == entries[0].Email {
		t.Error("the row carries the address")
	}
	if len(rows[0][0]) != 64 {
		t.Errorf("the code name is %d characters, want 64", len(rows[0][0]))
	}
	if rows[0][1] != "team-alpha" {
		t.Errorf("the team is %q, want team-alpha", rows[0][1])
	}
}

// Without a salt the directory cannot be hashed, and the command says so rather than storing
// something unusable.
func TestHashDirectoryRefusesWithoutASalt(t *testing.T) {
	if _, err := HashDirectory(nil, "", "v1"); err == nil {
		t.Error("a directory was hashed with no salt")
	}
}

// The source hash is the SHA-256 of the file, so the same file always gives the same value and a
// different file never does.
func TestSourceHashIsStableAndDifferent(t *testing.T) {
	a := SourceHashOf("email,team\na@example.com,alpha\n")
	if a == "" {
		t.Fatal("the source hash is empty")
	}
	if a != SourceHashOf("email,team\na@example.com,alpha\n") {
		t.Error("the same file gave two different hashes")
	}
	if a == SourceHashOf("email,team\nb@example.com,alpha\n") {
		t.Error("two different files gave the same hash")
	}
}
