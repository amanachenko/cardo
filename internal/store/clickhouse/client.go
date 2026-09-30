// Package clickhouse implements the ADR-0009 reference storage target.
//
// It talks to ClickHouse over the HTTP interface using only the standard library (ADR-0022), so
// `cardo` stays a single static binary with no dependencies and no cgo. The volumes involved are
// one row per actor per day; the native protocol's throughput advantage buys nothing here.
package clickhouse

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Database is fixed rather than configurable, per ADR-0020. The migrations qualify every object
// with it, so a view cannot accidentally resolve against whatever database a session happens to
// be pointed at.
const Database = "cardo"

// Client is a minimal ClickHouse HTTP client: execute a statement, run a query, insert rows.
type Client struct {
	endpoint *url.URL
	user     string
	password string
	http     *http.Client
}

// Config carries the connection settings. Every field comes from the environment or a flag; none
// of it is compiled in. The destination is the operator's own ClickHouse inside their own network
// (INV-7), and only they know its address.
type Config struct {
	// Endpoint is the ClickHouse HTTP interface, port 8123 by default on a stock server.
	Endpoint string
	User     string
	Password string
	Timeout  time.Duration
}

// NewClient validates the configuration and returns a client.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf(
			"no ClickHouse endpoint configured.\n" +
				"  Set CARDO_CLICKHOUSE_URL to your server's HTTP interface -- scheme, host and\n" +
				"  port, where a stock ClickHouse listens on 8123. There is no default: the\n" +
				"  destination is your infrastructure and cardo will not guess at it")
	}
	// Checked before parsing, because url.Parse answers a scheme-less host:port with a message
	// about path segments and colons, which tells an operator nothing about what to change.
	if !strings.Contains(cfg.Endpoint, "://") {
		return nil, fmt.Errorf(
			"ClickHouse endpoint %q has no scheme.\n"+
				"  Give the full HTTP interface URL, scheme included: http or https, then the\n"+
				"  host and port (8123 on a stock server)", cfg.Endpoint)
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing ClickHouse endpoint %q: %w", cfg.Endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf(
			"ClickHouse endpoint %q has scheme %q; expected the HTTP interface.\n"+
				"  This is the HTTP port (8123 by default), not the native protocol port (9000)",
			cfg.Endpoint, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("ClickHouse endpoint %q has no host", cfg.Endpoint)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		endpoint: u,
		user:     cfg.User,
		password: cfg.Password,
		http:     &http.Client{Timeout: timeout},
	}, nil
}

// Host returns the destination host, for logging. The operator should be able to read the
// destination of their engineering telemetry off the first line of output.
func (c *Client) Host() string { return c.endpoint.Host }

// IsPrivateDestination reports whether the endpoint is a loopback or private-range IP literal.
//
// A false result is not an error and does not block anything -- plenty of organizations run
// internal services on publicly routable addresses, and cardo cannot tell an internal address
// from an external one by looking at it. It exists so the caller can say out loud that
// pseudonymous engineering telemetry is about to be written to a public address, which is the
// kind of thing INV-7 exists to make visible rather than silent.
func (c *Client) IsPrivateDestination() bool {
	host := c.endpoint.Hostname()
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname. Resolving it here would be a DNS lookup at config time whose answer can
		// change later, so report unknown as "not obviously private" and let the caller say so.
		//
		// One shape of hostname is knowable without DNS: a single label with no dot. Dotless
		// names cannot exist in public DNS, so one only resolves through something local -- a
		// container network's service name, /etc/hosts, a search domain. The reference stack
		// reaches ClickHouse as "clickhouse", and warning about that on every start taught
		// people to ignore the warning.
		return strings.EqualFold(host, "localhost") || (host != "" && !strings.Contains(host, "."))
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// do posts a request body to ClickHouse with the given query-string parameters.
func (c *Client) do(ctx context.Context, params url.Values, body io.Reader) ([]byte, error) {
	u := *c.endpoint
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return nil, err
	}
	if c.user != "" {
		req.Header.Set("X-ClickHouse-User", c.user)
	}
	if c.password != "" {
		req.Header.Set("X-ClickHouse-Key", c.password)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching ClickHouse at %s: %w", c.endpoint.Host, err)
	}
	defer resp.Body.Close()

	out, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &Error{
			Status:  resp.StatusCode,
			Code:    resp.Header.Get("X-ClickHouse-Exception-Code"),
			Message: strings.TrimSpace(string(out)),
			Host:    c.endpoint.Host,
		}
	}
	if readErr != nil {
		return nil, fmt.Errorf("reading ClickHouse response: %w", readErr)
	}
	return out, nil
}

// Exec runs a statement that returns no rows.
func (c *Client) Exec(ctx context.Context, statement string) error {
	_, err := c.do(ctx, url.Values{}, strings.NewReader(statement))
	return err
}

// Query runs a statement and returns the raw response body. The caller chooses the format by
// ending the statement with a FORMAT clause.
func (c *Client) Query(ctx context.Context, statement string) ([]byte, error) {
	return c.do(ctx, url.Values{}, strings.NewReader(statement))
}

// Insert writes newline-delimited JSON rows into a table.
//
// Both settings are sent explicitly rather than relied on as defaults, because both defaults have
// changed underneath this code at least once and neither failure would be visible.
//
// `input_format_skip_unknown_fields=0`: if the Go row struct ever grows a field the table does not
// have, this makes ClickHouse reject the insert instead of silently discarding the column. A store
// that quietly drops data it was asked to persist is the failure mode worth paying a loud error to
// avoid.
//
// `async_insert=0`: ClickHouse 26.x ships with async inserts ON by default, which was verified on a
// running 26.6 server rather than read. Async insert exists to coalesce many small inserts from many
// concurrent clients; cardo writes one batch per source per day, so it buys nothing here. It is
// switched off for correctness rather than for speed. Put() inserts into a staging table and then
// hands the rows over with REPLACE PARTITION, and that sequence is only safe while the INSERT has
// genuinely landed before the swap runs. Today `wait_for_async_insert` defaults to 1 and it does.
// Relying on that leaves a day's data one server-side default away from being swapped in empty --
// silently, on a path with no error to report. The setting cannot be left to the server: the
// destination is the operator's own ClickHouse (INV-7) and they may never have seen cardo's
// deployment overlay, so the guarantee has to travel with the binary.
func (c *Client) Insert(ctx context.Context, table string, rows []byte) error {
	params := url.Values{
		"query":                            {fmt.Sprintf("INSERT INTO %s FORMAT JSONEachRow", table)},
		"input_format_skip_unknown_fields": {"0"},
		"async_insert":                     {"0"},
	}
	_, err := c.do(ctx, params, bytes.NewReader(rows))
	return err
}

// Ping checks that the server is reachable and answering queries.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.Query(ctx, "SELECT 1"); err != nil {
		return err
	}
	return nil
}

// Close releases the idle connection pool.
func (c *Client) Close() error {
	c.http.CloseIdleConnections()
	return nil
}
