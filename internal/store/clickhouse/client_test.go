package clickhouse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The endpoint is the one piece of configuration an operator is most likely to get wrong, and the
// two wrong answers are predictable: an empty value, and the native protocol port.
func TestNewClientRejectsUnusableEndpoints(t *testing.T) {
	for _, tc := range []struct{ name, endpoint, wantIn string }{
		{"empty", "", "CARDO_CLICKHOUSE_URL"},
		{"no scheme", "127.0.0.1:8123", "no scheme"},
		{"native protocol", "tcp://127.0.0.1:9000", "HTTP interface"},
		{"scheme only", "http://", "no host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(Config{Endpoint: tc.endpoint})
			if err == nil {
				t.Fatal("expected a refusal, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error does not mention %q: %v", tc.wantIn, err)
			}
		})
	}
}

// INV-7 makes the destination of the data worth stating out loud. Cardo cannot tell an internal
// address from an external one, so this only distinguishes the obviously-internal case -- and the
// test pins that it does not over-claim.
func TestIsPrivateDestination(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		private  bool
	}{
		{"http://127.0.0.1:8123", true},
		{"http://localhost:8123", true},
		{"http://10.1.2.3:8123", true},
		{"http://192.168.1.10:8123", true},
		{"http://172.16.0.9:8123", true},
		// A dotless name cannot be in public DNS: it is a container service name or a local alias.
		{"http://clickhouse:8123", true},
		{"https://203.0.113.10:8443", false},
		// A hostname is unknowable without DNS, and a DNS answer taken now can change later.
		// Reporting "not obviously private" is the honest answer, not a false negative.
		{"https://clickhouse.internal.example", false},
	} {
		c, err := NewClient(Config{Endpoint: tc.endpoint})
		if err != nil {
			t.Fatalf("%s: %v", tc.endpoint, err)
		}
		if got := c.IsPrivateDestination(); got != tc.private {
			t.Errorf("%s: IsPrivateDestination() = %v, want %v", tc.endpoint, got, tc.private)
		}
	}
}

func TestErrorCarriesAnOperatorHint(t *testing.T) {
	for _, tc := range []struct{ code, wantIn string }{
		{"516", "CARDO_CLICKHOUSE_USER"},
		{"497", "permission"},
		{"81", "migration"},
		{"60", "Re-run the poller"},
	} {
		e := &Error{Status: 500, Code: tc.code, Message: "DB::Exception: something\nstack frame\nstack frame", Host: "h"}
		got := e.Error()
		if !strings.Contains(got, tc.wantIn) {
			t.Errorf("exception %s: hint missing %q, got:\n%s", tc.code, tc.wantIn, got)
		}
		if strings.Contains(got, "stack frame") {
			t.Errorf("exception %s: the server stack trace was not trimmed:\n%s", tc.code, got)
		}
	}
}

// The insert path must refuse to silently discard a column. If the Go row struct grows a field the
// table does not have, that is a schema drift the operator needs to see, not lose.
func TestInsertRefusesToDropUnknownFields(t *testing.T) {
	var gotQuery, gotSkip, gotAsync, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		gotSkip = r.URL.Query().Get("input_format_skip_unknown_fields")
		gotAsync = r.URL.Query().Get("async_insert")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()

	c, err := NewClient(Config{Endpoint: srv.URL, User: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Insert(context.Background(), "cardo.t", []byte(`{"a":1}`+"\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "INSERT INTO cardo.t FORMAT JSONEachRow") {
		t.Errorf("unexpected insert statement: %q", gotQuery)
	}
	if gotAsync != "0" {
		t.Errorf("async_insert = %q, want %q.\n"+
			"  Put() inserts into a staging table and then swaps it in with REPLACE PARTITION.\n"+
			"  An asynchronous insert is only safe there while wait_for_async_insert stays 1; if\n"+
			"  that server default ever changes, the swap can run before the rows land and the\n"+
			"  day is published empty, with no error anywhere. ClickHouse 26.x already flipped\n"+
			"  async_insert on by default, so this is not hypothetical drift.", gotAsync, "0")
	}
	if gotSkip != "0" {
		t.Errorf("input_format_skip_unknown_fields = %q, want %q -- a store that quietly drops "+
			"data it was asked to persist is the failure this setting prevents", gotSkip, "0")
	}
	if gotBody != `{"a":1}`+"\n" {
		t.Errorf("body = %q", gotBody)
	}
}

func TestCredentialsTravelAsHeadersNotQueryParameters(t *testing.T) {
	var rawQuery, user, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		user = r.Header.Get("X-ClickHouse-User")
		key = r.Header.Get("X-ClickHouse-Key")
	}))
	defer srv.Close()

	c, _ := NewClient(Config{Endpoint: srv.URL, User: "cardo", Password: "s3cret"})
	if err := c.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if user != "cardo" || key != "s3cret" {
		t.Errorf("credentials did not travel as headers: user=%q key=%q", user, key)
	}
	// A password in a query string ends up in ClickHouse's own query_log and in any proxy access
	// log between here and there.
	if strings.Contains(rawQuery, "s3cret") {
		t.Errorf("the password appeared in the query string: %q", rawQuery)
	}
}

func TestSplitStatementsIgnoresSemicolonsInComments(t *testing.T) {
	got := splitStatements("-- a comment; with a semicolon\nSELECT 1;\n\nSELECT 2;\n")
	if len(got) != 2 || got[0] != "SELECT 1" || !strings.HasPrefix(got[1], "SELECT 2") {
		t.Errorf("splitStatements = %#v", got)
	}
}

// A migration checksum survives git rewriting line endings, and still catches a real edit.
func TestMigrationChecksumIgnoresLineEndings(t *testing.T) {
	lf := []byte("CREATE VIEW a AS\nSELECT 1;\n")
	crlf := []byte("CREATE VIEW a AS\r\nSELECT 1;\r\n")
	if migrationChecksum(lf) != migrationChecksum(crlf) {
		t.Error("the same migration has two checksums depending on its line endings")
	}
	// Installs recorded the raw bytes' hash before normalization, from either kind of checkout.
	for name, recorded := range map[string][]byte{"LF": lf, "CRLF": crlf} {
		sum := sha256.Sum256(recorded)
		prior := hex.EncodeToString(sum[:])
		for _, now := range [][]byte{lf, crlf} {
			if !checksumMatches(prior, now) {
				t.Errorf("a checksum recorded from a %s checkout no longer matches after git flipped the file", name)
			}
		}
	}
	if checksumMatches(migrationChecksum(lf), []byte("CREATE VIEW a AS\nSELECT 2;\n")) {
		t.Error("an edited migration matched the checksum of the original")
	}
}
