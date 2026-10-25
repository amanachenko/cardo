// Package pricing prices tokens from a table of Anthropic's published prices. USD per token.
//
// The table is static: Cardo never contacts a price service, so nothing about an organization ever
// leaves its network to be billed (INV-7). A model that is not in the table is unpriced, and the
// surface that shows it says so rather than guessing a rate.
//
// The prices below were taken from Anthropic's published pricing page on 2026-10-25
// (platform.claude.com/docs/en/about-claude/pricing): base input, output, the 5-minute
// cache write and the cache read, per million tokens. Cache writes are billed at 1.25x the input
// rate and cache reads at 0.1x it, except for Opus 5.5 and Sonnet 5.5 (0.05x) and Fable 5.1 and
// Mythos 5.1 (0.025x).
package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Price struct {
	Prompt     float64
	Completion float64
	CacheRead  float64
	CacheWrite float64
}

type Table map[string]Price

// perMTok is the published price per million tokens, as Anthropic states it.
type perMTok struct {
	prompt     float64
	completion float64
	cacheRead  float64
	cacheWrite float64
}

// published is Anthropic's price list, per million tokens.
var published = map[string]perMTok{
	"claude-fable-5-1":  {10, 50, 0.25, 12.50},
	"claude-mythos-5-1": {10, 50, 0.25, 12.50},
	"claude-fable-5":    {10, 50, 1, 12.50},
	"claude-mythos-5":   {10, 50, 1, 12.50},
	"claude-opus-5-5":   {4, 20, 0.20, 5},
	"claude-opus-5":     {5, 25, 0.50, 6.25},
	"claude-opus-4-8":   {5, 25, 0.50, 6.25},
	"claude-opus-4-7":   {5, 25, 0.50, 6.25},
	"claude-opus-4-6":   {5, 25, 0.50, 6.25},
	"claude-opus-4-5":   {5, 25, 0.50, 6.25},
	"claude-opus-4-1":   {15, 75, 1.50, 18.75},
	"claude-opus-4":     {15, 75, 1.50, 18.75},
	"claude-sonnet-5-5": {2, 10, 0.10, 2.50},
	"claude-sonnet-5":   {2, 10, 0.20, 2.50},
	"claude-sonnet-4-6": {3, 15, 0.30, 3.75},
	"claude-sonnet-4-5": {3, 15, 0.30, 3.75},
	"claude-sonnet-4":   {3, 15, 0.30, 3.75},
	"claude-haiku-5-5":  {0.10, 0.50, 0.01, 0.125},
	"claude-haiku-4-5":  {1, 5, 0.10, 1.25},
	"claude-haiku-3-5":  {0.80, 4, 0.08, 1},
}

// overridePath is where a local price table may replace the published one. It is an escape hatch
// for an organization whose contract differs from the list price; it is never fetched.
//
// The file name says "override" on purpose: an older Cardo cached a fetched catalogue under
// "pricing.json", and a stale cache must never be read as an organization's own price list.
func overridePath() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "cardo", "pricing-override.json")
}

// Load returns the price table: the published one, or a local override when one exists.
func Load() Table {
	if t, err := read(overridePath()); err == nil && len(t) > 0 {
		return withAliases(t)
	}
	t := Table{}
	for id, p := range published {
		t[id] = Price{
			Prompt:     p.prompt / 1e6,
			Completion: p.completion / 1e6,
			CacheRead:  p.cacheRead / 1e6,
			CacheWrite: p.cacheWrite / 1e6,
		}
	}
	return t
}

// withAliases adds the normalized name of every id, so an override written with a vendor prefix or
// a date suffix resolves the same way a published one does.
func withAliases(t Table) Table {
	out := Table{}
	for id, p := range t {
		out[id] = p
		if key := normalize(id); key != "" {
			if _, ok := out[key]; !ok {
				out[key] = p
			}
		}
	}
	return out
}

// Resolve finds a price for a transcript model name: the exact id first, then the id without its
// date suffix, then the bare name against the vendor-prefixed ids. It refuses to guess when
// several models match.
func (t Table) Resolve(model string) (Price, bool) {
	key := normalize(model)
	if key == "" {
		return Price{}, false
	}
	if p, ok := t[key]; ok {
		return p, true
	}
	var ids []string
	for id := range t {
		if strings.HasSuffix(id, "-"+key) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 1 {
		return t[ids[0]], true
	}
	sort.Strings(ids)
	return Price{}, false
}

// normalize turns a model name into a key: lower case, without a vendor prefix, and without the
// date suffix Anthropic puts on a released model ("claude-haiku-4-5-20251001").
func normalize(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" || strings.HasPrefix(key, "<") {
		return ""
	}
	if i := strings.LastIndex(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	// A date suffix is eight digits after the last dash: 20251001.
	if i := strings.LastIndex(key, "-"); i > 0 {
		if tail := key[i+1:]; len(tail) == 8 && isDigits(tail) {
			key = key[:i]
		}
	}
	return key
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// Cost bills tokens: prompt, completion, cache reads and cache writes.
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
