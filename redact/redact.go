// Package redact masks well-known credential-shaped field names before
// a value is handed to a logger. It is deliberately conservative: only
// keys whose name indicates a credential are masked, and the original
// value is never mutated. Callers that log user data should route it
// through Map first; callers that log their own internal state do not
// need to.
package redact

import "strings"

// Placeholder is the value every masked field becomes. It is exported so
// tests and downstream sinks can recognize a redacted field without
// guessing the format.
const Placeholder = "[REDACTED]"

// sensitiveKeys is matched case-insensitively as a substring against
// the field name. The list errs on the side of over-matching: masking
// an extra field costs a small amount of log signal, missing a
// credential costs an incident.
var sensitiveKeys = [...]string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"token",
	"api_key",
	"apikey",
	"api-key",
	"access_key",
	"access-key",
	"refresh_token",
	"client_secret",
	"private_key",
	"privatekey",
	"authorization",
	"bearer",
	"cookie",
	"credential",
	"oauth",
}

// Map returns a shallow copy of m with credential-shaped values masked.
// Nested maps and slices are redacted recursively. The input is not
// mutated; callers can log the result and keep using the original.
func Map(m map[string]any) map[string]any {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = redactValue(key, value)
	}
	return out
}

func redactValue(key string, value any) any {
	if isSensitiveKey(key) {
		return Placeholder
	}
	return redactDeep(value)
}

func redactDeep(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return Map(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactDeep(item)
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range sensitiveKeys {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
