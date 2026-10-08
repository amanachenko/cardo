package pseudonym

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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

// ErrIdentityLeak is returned when the raw payload still contains the actor identifier, or any
// email address, after scrubbing. It means a source carries identity somewhere this code does not
// know about, and it is deliberately fatal: a partial scrub is the exact failure INV-2 exists to
// prevent, and continuing would write a real email address into bronze. It does not say which
// address it found, because an error ends up in a log.
type ErrIdentityLeak struct {
	Source source.Name
	Day    time.Time
}

func (e ErrIdentityLeak) Error() string {
	return fmt.Sprintf(
		"identity leak: %s payload for %s still contains the actor identifier or an email address "+
			"after scrubbing; the source carries identity in a field this adapter does not model (INV-2)",
		e.Source, e.Day.Format("2006-01-02"))
}

// emailShape is the pattern the collector's INV-2 tripwire stops on (deploy/collector/config.yaml),
// so that both paths stop on the same thing.
var emailShape = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)

// Scrub hashes the actor identifier and removes it from the raw payload.
//
// The raw payload is rewritten rather than passed through: the actor key named by ActorPath is
// deleted and replaced by a pseudonym sibling, and the keys named by OtherIdentity are deleted, so
// bronze keeps a faithful copy of everything the source said except who said it.
func (h *Hasher) Scrub(r source.Record) (Scrubbed, error) {
	pseudo, err := h.Hash(r.ActorID)
	if err != nil {
		return Scrubbed{}, fmt.Errorf("hashing actor for %s %s: %w", r.Source, r.Day.Format("2006-01-02"), err)
	}

	raw, err := rewriteActor(r.Raw, r.ActorPath, r.OtherIdentity, pseudo, h.version)
	if err != nil {
		return Scrubbed{}, fmt.Errorf("scrubbing %s payload for %s: %w", r.Source, r.Day.Format("2006-01-02"), err)
	}

	// Safety net. rewriteActor removes the fields the adapter declared; this catches identity
	// appearing anywhere else in the payload, which is how an upstream schema change would first
	// show up. The actor's own identifier is compared case-insensitively because email casing is
	// not stable, and any other email address stops the run too: someone else's address in an
	// unmodelled field is as much a leak, and an API key actor's identifier is not an address.
	norm := strings.ToLower(strings.TrimSpace(r.ActorID))
	if (norm != "" && strings.Contains(strings.ToLower(string(raw)), norm)) || emailShape.Match(raw) {
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

// rewriteActor deletes the key at path and writes pseudonym fields into its parent object, and
// deletes the key at each path in other.
//
// Everything else is written back as the source sent it, bar key order and whitespace. Numbers
// keep their literal text, so an integer past 2^53 is not rounded through float64, and <, > and &
// are not rewritten as \u escapes.
func rewriteActor(raw json.RawMessage, path []string, other [][]string, pseudo, saltVersion string) (json.RawMessage, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("adapter declared no actor path; refusing to store a payload whose identity field is unknown")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding payload: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("decoding payload: more after the first JSON value")
	}
	if doc == nil {
		return nil, fmt.Errorf("decoding payload: not a JSON object")
	}

	parent, err := parentOf(doc, path)
	if err != nil {
		return nil, err
	}
	delete(parent, path[len(path)-1])
	parent["pseudonym"] = pseudo
	parent["pseudonym_salt_version"] = saltVersion

	for _, p := range other {
		parent, err := parentOf(doc, p)
		if err != nil {
			return nil, err
		}
		delete(parent, p[len(p)-1])
	}

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("re-encoding payload: %w", err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// parentOf returns the object holding the last key of an identity path.
func parentOf(doc map[string]any, path []string) (map[string]any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("adapter declared an empty identity path")
	}
	parent := doc
	for _, key := range path[:len(path)-1] {
		next, ok := parent[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("identity path %q does not resolve to an object", strings.Join(path, "."))
		}
		parent = next
	}
	return parent, nil
}
