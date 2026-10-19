// Package retention deletes raw rows older than the operator's window. Silver and gold are views,
// so only bronze is touched; a shorter window therefore shortens every downstream number at once
// (ADR-0012).
package retention

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Table is one bronze table and the predicate that ages it.
type Table struct {
	Name string
	Age  string // a SQL predicate over the table, e.g. "day < today() - 30"
	TTL  string // the expression the table's TTL is built on, e.g. "day"
}

// Tables is the fixed list retention owns.
func Tables(days int) []Table {
	return []Table{
		{Name: "cardo.bronze_actor_day", Age: fmt.Sprintf("day < today() - %d", days), TTL: "day"},
		{Name: "cardo.bronze_actor_day_staging", Age: fmt.Sprintf("day < today() - %d", days), TTL: "day"},
		{Name: "cardo.bronze_otel_logs", Age: fmt.Sprintf("Timestamp < now() - INTERVAL %d DAY", days), TTL: "toDate(Timestamp)"},
		{Name: "cardo.bronze_hook_events", Age: fmt.Sprintf("Timestamp < now() - INTERVAL %d DAY", days), TTL: "toDate(Timestamp)"},
		{Name: "cardo.bronze_otel_metrics_sum", Age: fmt.Sprintf("TimeUnix < now() - INTERVAL %d DAY", days), TTL: "toDate(TimeUnix)"},
	}
}

// Report says how many rows each table would delete, or did.
type Report map[string]int64

// Prune deletes rows older than days. With dryRun it only counts.
func Prune(ctx context.Context, endpoint, user, pass string, days int, dryRun bool) (Report, error) {
	if days <= 0 {
		return nil, fmt.Errorf("retention window must be at least one day")
	}
	out := Report{}
	for _, t := range Tables(days) {
		if !dryRun && t.TTL != "" {
			// The table ships with a 90-day TTL; the operator's window wins, in both directions.
			if err := exec(ctx, endpoint, user, pass, fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s + toIntervalDay(%d)", t.Name, t.TTL, days)); err != nil {
				return out, fmt.Errorf("%s: %w", t.Name, err)
			}
		}
		n, err := count(ctx, endpoint, user, pass, fmt.Sprintf("SELECT count() FROM %s WHERE %s", t.Name, t.Age))
		if err != nil {
			return out, fmt.Errorf("%s: %w", t.Name, err)
		}
		out[t.Name] = n
		if dryRun || n == 0 {
			continue
		}
		if err := exec(ctx, endpoint, user, pass, fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s SETTINGS mutations_sync=1", t.Name, t.Age)); err != nil {
			return out, fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	return out, nil
}

func count(ctx context.Context, endpoint, user, pass, sql string) (int64, error) {
	body, err := query(ctx, endpoint, user, pass, sql)
	if err != nil {
		return 0, err
	}
	var n int64
	_, err = fmt.Sscanf(strings.TrimSpace(body), "%d", &n)
	return n, err
}

func exec(ctx context.Context, endpoint, user, pass, sql string) error {
	_, err := query(ctx, endpoint, user, pass, sql)
	return err
}

func query(ctx context.Context, endpoint, user, pass, sql string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("ClickHouse endpoint is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", pass)
	req.Header.Set("Content-Type", "text/plain")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("clickhouse returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}
