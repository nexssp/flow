package dslparse

import (
	"fmt"
	"strings"
)

// modHandler applies one modifier value to a Modifiers struct.
//
//	hasValue=false → boolean flag, e.g. :coalesce
//	hasValue=true  → key=value, e.g. :timeout=30s
type modHandler func(p *Modifiers, value string, hasValue bool) error

var modHandlers = map[string]modHandler{}

// registerMod is called from init() of each mod_*.go file. Duplicate
// keys panic at load time, not at runtime under an unlucky input.
func registerMod(key string, h modHandler) {
	if _, dup := modHandlers[key]; dup {
		panic("dslparse: duplicate modifier :" + key)
	}
	modHandlers[key] = h
}

// applyModifiers dispatches each raw modifier to its handler. Unknown
// modifiers fall through to the transport binding builder, and if that
// also does not recognize the key, the modifier is silently ignored
// for backward compatibility.
//
// A future version may promote "unknown modifier" to a compile error;
// the handler registry already provides everything needed for that.
func applyModifiers(mods []string, p *Modifiers) error {
	for _, rawMod := range mods {
		key, value, hasValue := splitModString(rawMod)

		if h, ok := modHandlers[key]; ok {
			if err := h(p, value, hasValue); err != nil {
				return fmt.Errorf(":%s: %w", key, err)
			}
			continue
		}

		if hasValue {
			val := trimValue(value)
			if b := buildDSLTransportBinding(key, val); b != nil {
				p.Transports = append(p.Transports, *b)
				continue
			}
		}
	}
	return nil
}

// splitModString parses ":key=value" or ":key" into ("key", "value", true/false).
// The leading colon is already stripped by the caller.
func splitModString(s string) (key, value string, hasValue bool) {
	s = strings.TrimSpace(s)
	if eq := indexByte(s, '='); eq >= 0 {
		return toLower(strings.TrimSpace(s[:eq])), s[eq+1:], true
	}
	return toLower(s), "", false
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
