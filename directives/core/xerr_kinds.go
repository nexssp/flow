package core

// KnownXerrKinds lists the xerr.Kind string values the DSL accepts in
// `@on_error { when error.kind == "X" }` and `@retry NAME { only: [X] }`.
// Hand-maintained rather than reflected from kernel/xerr — the DSL is a
// stable surface and adding a kind should be deliberate.
func KnownXerrKinds() []string {
	return []string{
		"BadRequest", "Unauthorized", "Forbidden", "NotFound",
		"Conflict", "Validation", "TooManyRequests", "Timeout",
		"Unavailable", "Internal", "MethodNotAllowed", "RateLimit",
		"Canceled", "Database", "Shutdown", "CircuitBreaker",
	}
}

// IsKnownXerrKind reports whether k is one of the known kinds.
func IsKnownXerrKind(k string) bool {
	for _, known := range KnownXerrKinds() {
		if k == known {
			return true
		}
	}
	return false
}
