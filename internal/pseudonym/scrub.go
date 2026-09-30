package pseudonym

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/amanachenko/cardo/internal/source"
)

// Scrubbed is a record that is safe to persist.
//
// Its fields are unexported and it has no exported constructor, so the only way to obtain one is
// Hasher.Scrub. That makes INV-2 — "user.email is never persisted; pseudonymization happens before
// storage, always" — a property the compiler checks, rather than a rule a future change could
// quietly step around. Stores accept this type and nothing else.
type Scrubbed struct {
	source       source.Name
	day          time.Time
	pseudonym    string
	saltVersion  string
	actorType    string
	orgID        string
	customerType string
	terminalType string
	raw          json.RawMessage
	unknown      []string
}

func (s Scrubbed) Source() source.Name  { return s.source }
func (s Scrubbed) Day() time.Time       { return s.day }
func (s Scrubbed) Pseudonym() string    { return s.pseudonym }
func (s Scrubbed) SaltVersion() string  { return s.saltVersion }
func (s Scrubbed) ActorType() string    { return s.actorType }
func (s Scrubbed) OrgID() string        { return s.orgID }
func (s Scrubbed) CustomerType() string { return s.customerType }
func (s Scrubbed) TerminalType() string { return s.terminalType }
func (s Scrubbed) Raw() json.RawMessage { return s.raw }
func (s Scrubbed) Unknown() []string    { return s.unknown }

// ErrIdentityLeak is returned when the raw payload still contains the actor identifier after
// scrubbing. It means a source carries identity somewhere this code does not know about, and it is
// deliberately fatal: a partial scrub is the exact failure INV-2 exists to prevent, and continuing
// would write a real email address into bronze.
type ErrIdentityLeak struct {
	Source source.Name
	Day    time.Time
}

func (e ErrIdentityLeak) Error() string {
	return fmt.Sprintf(
		"identity leak: %s payload for %s still contains the actor identifier after scrubbing; "+
			"the source carries identity in a field this adapter does not model (INV-2)",
		e.Source, e.Day.Format("2006-01-02"))
}

// Scrub hashes the actor identifier and removes it from the raw payload.
//
// The raw payload is rewritten rather than passed through: the actor key named by ActorPath is
// deleted and replaced by a pseudonym sibling, so bronze keeps a faithful copy of everything the
// source said except who said it.
func (h *Hasher) Scrub(r source.Record) (Scrubbed, error) {
	pseudo, err := h.Hash(r.ActorID)
	if err != nil {
		return Scrubbed{}, fmt.Errorf("hashing actor for %s %s: %w", r.Source, r.Day.Format("2006-01-02"), err)
	}

	raw, err := rewriteActor(r.Raw, r.ActorPath, pseudo, h.version)
	if err != nil {
		return Scrubbed{}, fmt.Errorf("scrubbing %s payload for %s: %w", r.Source, r.Day.Format("2006-01-02"), err)
	}

	// Safety net. rewriteActor removes the one field the adapter declared; this catches identity
	// appearing anywhere else in the payload, which is how an upstream schema change would first
	// show up. Compared case-insensitively because email casing is not stable.
	if norm := strings.ToLower(strings.TrimSpace(r.ActorID)); norm != "" &&
		strings.Contains(strings.ToLower(string(raw)), norm) {
		return Scrubbed{}, ErrIdentityLeak{Source: r.Source, Day: r.Day}
	}

	return Scrubbed{
		source:       r.Source,
		day:          r.Day.UTC().Truncate(24 * time.Hour),
		pseudonym:    pseudo,
		saltVersion:  h.version,
		actorType:    r.ActorType,
		orgID:        r.OrgID,
		customerType: r.CustomerType,
		terminalType: r.TerminalType,
		raw:          raw,
		unknown:      r.Unknown,
	}, nil
}

// rewriteActor deletes the key at path and writes pseudonym fields into its parent object.
func rewriteActor(raw json.RawMessage, path []string, pseudo, saltVersion string) (json.RawMessage, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("adapter declared no actor path; refusing to store a payload whose identity field is unknown")
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decoding payload: %w", err)
	}

	parent := doc
	for _, key := range path[:len(path)-1] {
		next, ok := parent[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("actor path %q does not resolve to an object", strings.Join(path, "."))
		}
		parent = next
	}

	delete(parent, path[len(path)-1])
	parent["pseudonym"] = pseudo
	parent["pseudonym_salt_version"] = saltVersion

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("re-encoding payload: %w", err)
	}
	return out, nil
}
