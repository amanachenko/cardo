package clickhouse

import "fmt"

// Error is a ClickHouse server error, carried with an operator-facing hint where one is known.
//
// The hints exist because the failures that actually happen on a first install are boring and
// diagnosable -- wrong credentials, wrong port, a database that was never created -- and a raw
// server exception buried in a stack of wrapped errors sends the operator to a search engine
// instead of to the fix.
type Error struct {
	Status  int
	Code    string
	Message string
	Host    string
	Hint    string
}

func (e *Error) Error() string {
	hint := e.Hint
	if hint == "" {
		hint = hintFor(e.Code, e.Status)
	}
	msg := fmt.Sprintf("ClickHouse at %s returned HTTP %d", e.Host, e.Status)
	if e.Code != "" {
		msg += fmt.Sprintf(" (exception %s)", e.Code)
	}
	if e.Message != "" {
		msg += ": " + firstLine(e.Message)
	}
	if hint != "" {
		msg += "\n  " + hint
	}
	return msg
}

// firstLine trims a ClickHouse exception down to its first line. The server appends a full stack
// trace, which is noise in a CLI and hides the sentence that matters.
func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// hintFor maps the ClickHouse exception codes a first install actually hits.
func hintFor(code string, status int) string {
	switch code {
	case "516", "193":
		return "Authentication failed. Check CARDO_CLICKHOUSE_USER and CARDO_CLICKHOUSE_PASSWORD.\n" +
			"  A stock server has a 'default' user with no password; a hardened one does not."
	case "497":
		return "The user exists but lacks permission. Creating the schema needs CREATE DATABASE,\n" +
			"  CREATE TABLE and CREATE VIEW on the 'cardo' database; polling needs INSERT, SELECT\n" +
			"  and ALTER on it."
	case "81":
		return "The 'cardo' database does not exist. It is created by the migration runner, which\n" +
			"  runs automatically before the first write -- so seeing this means migrations were\n" +
			"  skipped or failed earlier."
	case "60":
		return "A table is missing. Re-run the poller: migrations are applied on every run and are\n" +
			"  safe to repeat."
	case "252":
		return "Too many parts. ClickHouse is being written to faster than it can merge, which for\n" +
			"  a daily poller means something is inserting in a loop."
	}
	if status == 404 {
		return "HTTP 404 from the endpoint. This usually means the URL points at something that is\n" +
			"  not ClickHouse's HTTP interface -- check the port (8123 on a stock server)."
	}
	return ""
}
