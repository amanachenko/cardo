package console

import (
	"encoding/json"
	"sort"
)

// unknownPaths reports JSON field paths present in a payload that this adapter does not model.
//
// ADR-0014 requires tolerant parsing: an unrecognised field must never crash the poller and must
// never vanish silently. Storage keeps the payload verbatim regardless, so drift here is a signal
// to update the canonical views rather than an ingest failure. Array indices are collapsed, so a
// new field on the hundredth model_breakdown entry is reported once, not a hundred times.
func unknownPaths(raw json.RawMessage) []string {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	seen := map[string]bool{}
	walk(doc, "", seen)

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func walk(node any, prefix string, seen map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if !knownPaths[path] {
				seen[path] = true
				// Do not descend into an unknown subtree. Reporting the root of the new
				// structure is the useful signal; enumerating everything beneath it is noise.
				continue
			}
			walk(child, path, seen)
		}
	case []any:
		for _, child := range v {
			walk(child, prefix, seen)
		}
	}
}
