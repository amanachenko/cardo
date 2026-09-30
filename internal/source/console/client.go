package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/amanachenko/cardo/internal/source"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
	endpointPath   = "/v1/organizations/usage_report/claude_code"

	// pageLimit is the documented maximum. Backfill is the dominant cost and the rate limit for
	// this endpoint is not documented, so we take the largest page rather than the most requests.
	pageLimit = 1000

	// maxPages bounds a single day's pagination. A cursor loop that never terminates would
	// otherwise spend an admin key's rate limit silently.
	maxPages = 10_000

	// maxAttempts bounds retries for one page.
	maxAttempts = 5
)

// Client polls the Claude Code Analytics API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
	log        *slog.Logger
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API host. Used by tests to point at an httptest server.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient overrides the HTTP client, for timeouts or transport instrumentation.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// New returns a Client. The API key is an Admin API key; it is never logged.
func New(apiKey string, log *slog.Logger, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("admin API key is required")
	}
	c := &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		// Anthropic's documentation asks integrations to identify themselves.
		userAgent: "Cardo/0.1 (https://github.com/amanachenko/cardo)",
		log:       log,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Name implements source.Adapter.
func (c *Client) Name() source.Name { return source.Console }

// Fetch returns every record for one UTC day, following pagination to completion.
func (c *Client) Fetch(ctx context.Context, day time.Time) ([]source.Record, error) {
	var (
		out    []source.Record
		cursor string
	)
	for pageNum := 0; ; pageNum++ {
		if pageNum >= maxPages {
			return nil, fmt.Errorf("pagination did not terminate after %d pages for %s", maxPages, day.Format("2006-01-02"))
		}

		p, body, err := c.fetchPage(ctx, day, cursor)
		if err != nil {
			return nil, err
		}

		recs, err := c.decodeRecords(p, body, day)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)

		if !p.HasMore || p.NextPage == nil || *p.NextPage == "" {
			return out, nil
		}
		cursor = *p.NextPage
	}
}

// fetchPage performs one request and returns the decoded envelope alongside the raw body, which is
// needed because storage keeps payloads verbatim (ADR-0010).
func (c *Client) fetchPage(ctx context.Context, day time.Time, cursor string) (*page, []byte, error) {
	q := url.Values{}
	q.Set("starting_at", day.UTC().Format("2006-01-02"))
	q.Set("limit", strconv.Itoa(pageLimit))
	if cursor != "" {
		q.Set("page", cursor)
	}

	endpoint := c.baseURL + endpointPath + "?" + q.Encode()

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt)
			var apiErr *APIError
			if errors.As(lastErr, &apiErr) && apiErr.RetryAfter > 0 {
				delay = apiErr.RetryAfter
			}
			c.log.Warn("retrying analytics request",
				"day", day.Format("2006-01-02"), "attempt", attempt+1, "delay", delay, "cause", lastErr)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		body, status, header, err := c.do(ctx, endpoint)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			lastErr = fmt.Errorf("requesting %s: %w", day.Format("2006-01-02"), err)
			continue
		}

		if status != http.StatusOK {
			apiErr := classify(status, body, day).(*APIError)
			apiErr.RetryAfter = retryAfter(header)
			if !apiErr.Retryable() {
				return nil, nil, apiErr
			}
			lastErr = apiErr
			continue
		}

		var p page
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, nil, fmt.Errorf("decoding response for %s: %w", day.Format("2006-01-02"), err)
		}
		return &p, body, nil
	}

	return nil, nil, fmt.Errorf("giving up on %s after %d attempts: %w",
		day.Format("2006-01-02"), maxAttempts, lastErr)
}

// do performs a single request and returns the body, status and headers.
func (c *Client) do(ctx context.Context, endpoint string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("reading response body: %w", err)
	}
	return body, resp.StatusCode, resp.Header, nil
}

// decodeRecords converts an envelope into source.Records, pairing each with its verbatim payload.
func (c *Client) decodeRecords(p *page, body []byte, day time.Time) ([]source.Record, error) {
	// Re-extract the raw elements so each record keeps its own untouched payload. Marshalling the
	// decoded struct back to JSON would silently drop any field this adapter does not model, which
	// is exactly what ADR-0010's raw-first rule exists to prevent.
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("re-reading payload for %s: %w", day.Format("2006-01-02"), err)
	}
	if len(envelope.Data) != len(p.Data) {
		return nil, fmt.Errorf("payload length mismatch for %s: %d typed vs %d raw",
			day.Format("2006-01-02"), len(p.Data), len(envelope.Data))
	}

	out := make([]source.Record, 0, len(p.Data))
	for i, rec := range p.Data {
		id, path := rec.Actor.identifier()
		if id == "" {
			return nil, fmt.Errorf("record %d for %s has actor type %q with no identifier; "+
				"refusing to store a row whose identity field is unknown (INV-2)",
				i, day.Format("2006-01-02"), rec.Actor.Type)
		}

		unknown := unknownPaths(envelope.Data[i])
		if len(unknown) > 0 {
			c.log.Warn("unmodelled fields in Claude Code Analytics response",
				"day", day.Format("2006-01-02"),
				"paths", unknown,
				"action", "stored verbatim in bronze; update the canonical views")
		}

		out = append(out, source.Record{
			Source:       source.Console,
			Day:          day.UTC().Truncate(24 * time.Hour),
			ActorType:    rec.Actor.Type,
			ActorID:      id,
			ActorPath:    path,
			OrgID:        rec.OrgID,
			CustomerType: rec.CustomerType,
			TerminalType: rec.TerminalType,
			Raw:          envelope.Data[i],
			Unknown:      unknown,
		})
	}
	return out, nil
}
