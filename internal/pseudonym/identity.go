package pseudonym

import (
	"fmt"
	"strings"
)

// Identity is what an operator needs to see: the value the views will join on, and the salt version
// it belongs to. It carries no email.
type Identity struct {
	Value   string
	Version string
}

// ForEmail normalises an address and hashes it with the same salt the collector uses.
func ForEmail(salt, version, email string) (Identity, error) {
	addr := strings.ToLower(strings.TrimSpace(email))
	if addr == "" {
		return Identity{}, fmt.Errorf("an email address is required")
	}
	at := strings.Index(addr, "@")
	if at < 1 || at == len(addr)-1 || !strings.Contains(addr[at+1:], ".") {
		return Identity{}, fmt.Errorf("%q does not look like an email address", email)
	}
	if strings.TrimSpace(salt) == "" {
		return Identity{}, fmt.Errorf("no salt: pass -salt or set CARDO_SALT")
	}
	h, err := New(salt, version)
	if err != nil {
		return Identity{}, err
	}
	value, err := h.Hash(addr)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Value: value, Version: h.Version()}, nil
}
