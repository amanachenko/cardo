package madeup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Where the made-up organization's sessions go: the made-up stack's collector, and nowhere else
// (ADR-0051, decision 1). The addresses are fixed, with no flag to change them, as the poller's
// endpoint is: a generator that could be aimed at another collector could fill the store of an
// organization that runs Cardo with invented sessions. test/madeup_test.go holds them to the ports
// deploy/compose/docker-compose.made-up.yml publishes, which no other stack uses.
const (
	HooksURL  = "http://127.0.0.1:8090/v1/hooks"
	HealthURL = "http://127.0.0.1:8090/health"
	OTLPURL   = "http://127.0.0.1:4320"
)

// TimeHeader carries a hook event's invented time. Only the made-up stack's collector reads it
// (deploy/compose/made-up/collector-made-up.yml); the shipped collector never does.
const TimeHeader = "X-Cardo-Made-Up-Time"

// HTTPSink sends to the made-up stack, with workers requests in flight at once.
type HTTPSink struct {
	client *http.Client
	jobs   chan post
	wg     sync.WaitGroup
	once   sync.Once
	err    error
	failed chan struct{}

	Hooks, LogRequests, MetricRequests, Retries atomic.Int64
}

type post struct {
	url    string
	body   []byte
	at     time.Time
	isHook bool
}

// NewHTTPSink checks that the made-up stack's collector answers, then starts the workers.
func NewHTTPSink(ctx context.Context, workers int) (*HTTPSink, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        2 * workers,
			MaxIdleConnsPerHost: 2 * workers,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HealthURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the made-up stack's collector does not answer on %s: %w. Start it with scripts/made-up.sh up", HealthURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the made-up stack's collector answered %d on %s", resp.StatusCode, HealthURL)
	}

	h := &HTTPSink{client: client, jobs: make(chan post, 4*workers), failed: make(chan struct{})}
	for range workers {
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			for p := range h.jobs {
				if h.failedYet() {
					continue
				}
				if err := h.send(ctx, p); err != nil {
					h.fail(err)
				}
			}
		}()
	}
	return h, nil
}

func (h *HTTPSink) Hook(at time.Time, body []byte) error {
	return h.enqueue(post{url: HooksURL, body: body, at: at, isHook: true})
}

func (h *HTTPSink) Logs(body []byte) error {
	return h.enqueue(post{url: OTLPURL + "/v1/logs", body: body})
}

func (h *HTTPSink) Metrics(body []byte) error {
	return h.enqueue(post{url: OTLPURL + "/v1/metrics", body: body})
}

// Close waits for every request to be answered, and returns the first failure.
func (h *HTTPSink) Close() error {
	close(h.jobs)
	h.wg.Wait()
	return h.err
}

func (h *HTTPSink) enqueue(p post) error {
	select {
	case <-h.failed:
		return h.err
	case h.jobs <- p:
		return nil
	}
}

func (h *HTTPSink) failedYet() bool {
	select {
	case <-h.failed:
		return true
	default:
		return false
	}
}

func (h *HTTPSink) fail(err error) {
	h.once.Do(func() { h.err = err; close(h.failed) })
}

// How long one request may keep being retried while the collector is busy.
const patience = 5 * time.Minute

// send posts one request. The generator sends far faster than Claude Code ever does, and the
// collector answers "sending queue is full" while ClickHouse catches up, so a 429, a 5xx or a
// dropped connection is retried, which keeps the load at the collector's pace. A refusal is not:
// the collector's REFUSED (a weak salt, or an email address left on a record) is a mistake in what
// was sent, and no retry fixes it. Neither is any other answer.
func (h *HTTPSink) send(ctx context.Context, p post) error {
	delay := 100 * time.Millisecond
	deadline := time.Now().Add(patience)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(p.body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if p.isHook {
			req.Header.Set(TimeHeader, strconv.FormatInt(p.at.UnixNano(), 10))
		}
		resp, err := h.client.Do(req)
		code := 0
		var answer []byte
		if err == nil {
			code = resp.StatusCode
			answer, _ = io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			if code == http.StatusOK {
				switch {
				case p.isHook:
					h.Hooks.Add(1)
				case p.url == OTLPURL+"/v1/logs":
					h.LogRequests.Add(1)
				default:
					h.MetricRequests.Add(1)
				}
				return nil
			}
		}
		refused := bytes.Contains(answer, []byte("REFUSED"))
		retry := !refused && (err != nil || code == http.StatusTooManyRequests || code >= 500)
		if !retry || time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("POST %s: %w", p.url, err)
			}
			if refused {
				return fmt.Errorf("POST %s was refused: %s. A made-up event carried something the "+
					"collector is right to refuse", p.url, bytes.TrimSpace(answer))
			}
			return fmt.Errorf("POST %s answered %d: %s", p.url, code, bytes.TrimSpace(answer))
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		h.Retries.Add(1)
		time.Sleep(delay)
		delay = min(2*delay, 2*time.Second)
	}
}
