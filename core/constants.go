package core

import "strings"

// ApplyConstants rewrites `${key}` references in lines[start:] to their
// declared values. It is the single substitution primitive for
// compile-time constants; both the @const directive handlers and the
// @include merge path call it.
//
// Substitution is textual and idempotent: an already-substituted value
// no longer contains a `${...}` pattern, so re-application over the
// same slice is a no-op. Lines that contain no `${` are skipped without
// a replacement pass, keeping the common case free of allocations.
//
// lines is mutated in place. start is a half-open lower bound: lines
// [0, start) are untouched.
func ApplyConstants(lines []string, start int, constants map[string]string) {
	if len(constants) == 0 || start >= len(lines) {
		return
	}

	replacements := make([]string, 0, len(constants)*2)
	for k, v := range constants {
		replacements = append(replacements, "${"+k+"}", v)
	}
	replacer := strings.NewReplacer(replacements...)

	for i := start; i < len(lines); i++ {
		if strings.Contains(lines[i], "${") {
			lines[i] = replacer.Replace(lines[i])
		}
	}
}
