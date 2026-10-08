// Package pseudonym implements the salted-hash identity contract from ADR-0006.
//
// INV-2 states that user.email is never persisted and that pseudonymization happens before
// storage, always. This package is the only place identity is transformed, and the only way to
// construct a value the store will accept.
package pseudonym

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// MinSaltLen is the shortest salt we will run with.
//
// A short salt is worse than useless here: the identifier space is organizational email addresses,
// which are low-entropy and guessable, so a weak salt makes the pseudonyms trivially reversible by
// anyone holding the hashes. Refusing to start is the correct behaviour — see ADR-0006.
//
// The salt must also be hex. Thirty-two characters of hex carry 128 bits; thirty-two characters
// of a passphrase may carry a fraction of that, and the length check cannot tell them apart. The
// collector applies the same rule, and the two must agree, or one salt would pseudonymize one
// path and be refused on the other. The salt is hashed as written, never decoded, so a hex salt
// already in use keeps every pseudonym it made.
const MinSaltLen = 32

var (
	ErrSaltTooShort = fmt.Errorf("salt must be at least %d characters", MinSaltLen)
	ErrSaltNotHex   = errors.New("salt must be hex, as `openssl rand -hex 32` prints it; the collector refuses any other")
	ErrNoVersion    = errors.New("salt version is required; it is stored beside every pseudonym so rotation stays possible")
	ErrEmptyActor   = errors.New("actor identifier is empty")
)

// Hasher turns a raw actor identifier into a stable pseudonym.
//
// The salt is held by the organization's security team and is never written to this repository, never
// logged, and never persisted alongside the data it protects (ADR-0006).
type Hasher struct {
	salt    []byte
	version string
}

// New returns a Hasher, or an error if the salt would not actually protect anything.
func New(salt, version string) (*Hasher, error) {
	if len(salt) < MinSaltLen {
		return nil, ErrSaltTooShort
	}
	// Whatever is left once hex digits are trimmed from both ends is a character that is not one.
	if strings.Trim(salt, "0123456789abcdefABCDEF") != "" {
		return nil, ErrSaltNotHex
	}
	if strings.TrimSpace(version) == "" {
		return nil, ErrNoVersion
	}
	return &Hasher{salt: []byte(salt), version: version}, nil
}

// Version is stored in a column beside every pseudonym from day one, so that changing the salt
// later is a data question rather than an impossible retroactive migration (ADR-0006).
func (h *Hasher) Version() string { return h.version }

// Hash returns the pseudonym for an actor identifier.
//
// The identifier is normalized first. Email addresses arrive with inconsistent casing and stray
// whitespace, and without normalization the same person would acquire several pseudonyms and
// silently appear as several engineers in every cohort statistic.
func (h *Hasher) Hash(actorID string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(actorID))
	if norm == "" {
		return "", ErrEmptyActor
	}
	// The separator keeps salt and identifier from running together, so that a salt ending in a
	// character the identifier could begin with cannot produce a colliding preimage.
	sum := sha256.Sum256(append(append([]byte{}, h.salt...), append([]byte{0x1f}, norm...)...))
	return hex.EncodeToString(sum[:]), nil
}

// String deliberately does not reveal the salt. Hashers end up in log lines and error values by
// accident; this makes that harmless.
func (h *Hasher) String() string {
	return fmt.Sprintf("pseudonym.Hasher{version:%q, salt:<redacted, %d bytes>}", h.version, len(h.salt))
}
