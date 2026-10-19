package launcher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// chQuery posts one statement over the ClickHouse HTTP interface and returns the body.
//
// Credentials travel as X-ClickHouse-User / X-ClickHouse-Key headers, never in the query string:
// a password in a URL ends up in ClickHouse's own query_log and in every proxy access log between
// here and the server (ADR-0022).
func chQuery(ctx context.Context, endpoint, user, password, sql string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", password)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	_, _ = io.Copy(&b, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return b.String(), resp.StatusCode, fmt.Errorf("%s: %s", resp.Status, firstLine(b.String()))
	}
	return b.String(), resp.StatusCode, nil
}

// chCount runs a statement that answers with one number.
func chCount(ctx context.Context, endpoint, user, password, sql string) (int, error) {
	body, _, err := chQuery(ctx, endpoint, user, password, sql)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscan(strings.TrimSpace(body), &n); err != nil {
		return 0, fmt.Errorf("query %q returned %q", sql, firstLine(body))
	}
	return n, nil
}
