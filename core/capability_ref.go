package core

import (
	"errors"
	"fmt"
	"strings"
)

// CapabilityRef is a statically resolved reference to an action, stream,
// or operator. Raw is the identifier as written in the DSL; Canonical is
// the name after translating native keywords. Both are held so
// diagnostics can point at the source form.
type CapabilityRef struct {
	Raw       string
	Canonical string
}

// ParseCapabilityRef parses a single bare identifier. Quoted values are
// rejected: a capability reference is grammar, not data.
func ParseCapabilityRef(raw string) (CapabilityRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return CapabilityRef{}, errors.New("empty capability reference")
	}
	switch trimmed[0] {
	case '"', '\'', '`':
		return CapabilityRef{}, fmt.Errorf(
			"capability reference must be a bare identifier, not a string: %s", raw)
	}
	canonical := trimmed
	if c, ok := TranslateKeyword(trimmed); ok {
		canonical = c
	}
	return CapabilityRef{Raw: trimmed, Canonical: canonical}, nil
}

// ParseCapabilityRefList parses `[noop, const]` into capability
// references. It reuses ParseList for splitting, then runs every entry
// through ParseCapabilityRef so quoted members are rejected.
func ParseCapabilityRefList(line string) ([]CapabilityRef, error) {
	raw := ParseList(line)
	if len(raw) == 0 {
		return nil, errors.New("empty capability reference list")
	}
	refs := make([]CapabilityRef, 0, len(raw))
	for _, item := range raw {
		ref, err := ParseCapabilityRef(item)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
