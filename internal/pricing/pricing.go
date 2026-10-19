// Package pricing turns OpenRouter's public model catalogue into a price table. USD per token, as
// the API reports it; the table is cached for a day so the importer and the coach agree.
package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Price struct {
	Prompt    float64
	Completion float64
	CacheRead  float64
	CacheWrite float64
}

type Table map[string]Price

type payload struct {
	Data []struct {
		ID      string            `json:"id"`
		Pricing map[string]json.RawMessage `json:"pricing"`
	} `json:"data"`
}

func cachePath() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "cardo", "pricing.json")
}

// Load returns a table, refreshing it from OpenRouter when the cache is older than a day.
func Load(ctx context.Context, maxAge time.Duration) (Table, error) {
	path := cachePath()
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < maxAge {
		if t, err := read(path); err == nil && len(t) > 0 {
			return t, nil
		}
	}
	t, err := fetch(ctx)
	if err != nil {
		if cached, cerr := read(path); cerr == nil && len(cached) > 0 {
			return cached, nil
		}
		return nil, err
	}
	_ = write(path, t)
	return t, nil
}

func fetch(ctx context.Context) (Table, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openrouter models: %s", resp.Status)
	}
	var p payload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	t := Table{}
	for _, m := range p.Data {
		t[m.ID] = Price{
			Prompt:     num(m.Pricing["prompt"]),
			Completion: num(m.Pricing["completion"]),
			CacheRead:  num(m.Pricing["input_cache_read"]),
			CacheWrite: num(m.Pricing["input_cache_write"]),
		}
	}
	return t, nil
}

func num(raw json.RawMessage) float64 {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		var f float64
		_, _ = fmt.Sscanf(str, "%g", &f)
		return f
	}
	var f float64
	_ = json.Unmarshal(raw, &f)
	return f
}

// Resolve finds a price for a transcript model name: exact id first, then the bare name against the
// vendor-prefixed ids. It refuses to guess when several vendors match.
func (t Table) Resolve(model string) (Price, bool) {
	if p, ok := t[model]; ok {
		return p, true
	}
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" || strings.HasPrefix(key, "<") {
		return Price{}, false
	}
	var ids []string
	for id := range t {
		if strings.HasSuffix(strings.ToLower(id), "/"+key) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 1 {
		return t[ids[0]], true
	}
	sort.Strings(ids)
	return Price{}, false
}

// Cost bills tokens the way OpenRouter does: prompt, completion, cache reads and cache writes.
func (p Price) Cost(in, out, cacheRead, cacheWrite int64) float64 {
	return float64(in)*p.Prompt + float64(out)*p.Completion +
		float64(cacheRead)*p.CacheRead + float64(cacheWrite)*p.CacheWrite
}

func read(path string) (Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return t, nil
}

func write(path string, t Table) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
