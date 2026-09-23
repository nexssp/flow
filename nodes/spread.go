package nodes

import (
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/xerr"
)

// Reserved identifiers exposed to projection expressions.
//
//	rootVar   — the raw, un-normalised input value
//	stateVar  — the normalised object form of the input
//
// `stateVar` is what the spread token `...` expands to. Both names are
// namespaced with `__` to keep them out of the way of ordinary user
// data. Inside a projection they always carry the reserved meaning,
// even if the caller's input happens to contain fields with the same
// names; on the output side those user fields are preserved verbatim.
const (
	rootVar  = "__root__"
	stateVar = "__state__"

	// spreadToken is the surface syntax for "keep the previous state".
	// It is only recognised at the very beginning of a projection body.
	spreadToken = "..."

	// spreadMergeName is the internal function that performs the actual
	// map merge. It is registered with expr-lang at package init and
	// must not clash with a user-defined identifier.
	spreadMergeName = "nexss_spread_merge"
)

// spreadMergeOpt registers the merge function with every compiled
// projection expression.
//
// Semantics: `nexss_spread_merge(base, override)` returns a fresh map
// with all keys of `base` and all keys of `override`, where the
// override wins on conflict. Both arguments may be nil; the result is
// always a non-nil map.
var spreadMergeOpt = expr.Function(
	spreadMergeName,
	func(params ...any) (any, error) {
		if len(params) != 2 {
			return nil, fmt.Errorf("%s: expected 2 arguments, got %d", spreadMergeName, len(params))
		}

		base, err := asAnyMap(params[0])
		if err != nil {
			return nil, fmt.Errorf("%s: base: %w", spreadMergeName, err)
		}

		override, err := asAnyMap(params[1])
		if err != nil {
			return nil, fmt.Errorf("%s: override: %w", spreadMergeName, err)
		}

		merged := make(map[string]any, len(base)+len(override))
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range override {
			merged[k] = v
		}

		return merged, nil
	},
)

// asAnyMap normalises the arguments of the merge function. Nil is
// treated as an empty map so that spread over a non-map input degrades
// gracefully to "just the override".
func asAnyMap(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("expected map[string]any, got %T", v)
}

// PreprocessSpread prepares a projection body for expr-lang.
//
// Three shapes are supported:
//
//  1. Bare expression. A body that does not look like a map literal
//     body is returned unchanged. This is what allows function calls
//     and pipelines to be used in a projection slot:
//
//     sortBy(findings, #.severity)
//     findings | filter(#.ok) | take(10)
//     count(items, #.active)
//
//  2. Map body without spread. A body that starts with `key:` (or
//     `"key":`, `'key':`, “ `key`: “) is wrapped in a map literal:
//
//     a: 1, b: 2         → { a: 1, b: 2 }
//
//  3. Spread. A body that begins with `...` starts from the previous
//     step's state and overrides only the listed fields:
//
//     ...                          → __state__
//     ..., attempt: attempt + 1    → nexss_spread_merge(__state__, { attempt: attempt + 1 })
//
// The spread token must be the first entry. A misplaced `...` produces
// a compile error with a helpful message rather than a cryptic
// expr-lang parse error.
func PreprocessSpread(body string) (string, bool, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "{}", false, nil
	}

	body = stripOuterBraces(body)

	rest, hasSpread := trimSpreadPrefix(body)
	if hasSpread {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return stateVar, true, nil
		}
		return fmt.Sprintf("%s(%s, { %s })", spreadMergeName, stateVar, rest), true, nil
	}

	if containsBareSpread(body) {
		return "", false, xerr.BadRequest(
			"projection: `...` must appear as the first entry, e.g. { ..., key: value }",
		)
	}

	// Not a spread. Decide whether the body is a map literal body or a
	// bare expression. Bare expressions are returned unchanged so that
	// callers can use array functions, pipes, and other expr-lang
	// constructs directly in a projection slot.
	if !looksLikeMapBody(body) {
		return body, false, nil
	}

	return "{ " + body + " }", false, nil
}

// looksLikeMapBody reports whether s begins with a map key followed by
// a colon. It recognises both unquoted identifiers and quoted strings
// as keys.
//
//	"a: 1"        → true
//	`"a": 1`      → true
//	"sortBy(x)"   → false
//	"foo.bar"     → false
//	"a == b"      → false
//
// Whitespace between the key and the colon is allowed. A leading
// identifier that is followed by anything other than an optional
// whitespace run and a colon is not considered a key.
func looksLikeMapBody(s string) bool {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	if i >= len(s) {
		return false
	}

	c := s[i]

	// Quoted key: `"key":`, `'key':`, `` `key`: ``
	if c == '"' || c == '\'' || c == '`' {
		end := i + 1
		for end < len(s) && s[end] != c {
			if s[end] == '\\' && end+1 < len(s) {
				end += 2
				continue
			}
			end++
		}
		if end >= len(s) {
			return false
		}
		end++
		for end < len(s) && (s[end] == ' ' || s[end] == '\t') {
			end++
		}
		return end < len(s) && s[end] == ':'
	}

	// Unquoted identifier key.
	if !isIdentStartByte(c) {
		return false
	}
	end := i
	for end < len(s) && isIdentByte(s[end]) {
		end++
	}
	for end < len(s) && (s[end] == ' ' || s[end] == '\t') {
		end++
	}
	return end < len(s) && s[end] == ':'
}

// stripOuterBraces removes a matched `{ ... }` pair around s. If s is
// not enclosed, or the outer brace closes before the end, s is
// returned unchanged. Strings are skipped so that `{ msg: "}" }` is
// handled correctly.
func stripOuterBraces(s string) string {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return s
	}

	depth := 0
	var inStr byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		if inStr != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}

		switch c {
		case '"', '\'', '`':
			inStr = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if i == len(s)-1 {
					return strings.TrimSpace(s[1 : len(s)-1])
				}
				// The outer brace closes before the end — not a clean
				// wrapper, so leave the input alone.
				return s
			}
		}
	}

	return s
}

// trimSpreadPrefix removes a leading `...` and any following comma
// from body. The token must end at a delimiter (whitespace, comma, or
// end of string) so that `..foo` and `...foo` are rejected.
func trimSpreadPrefix(body string) (string, bool) {
	if !strings.HasPrefix(body, spreadToken) {
		return body, false
	}

	after := body[len(spreadToken):]

	if after != "" {
		switch after[0] {
		case ',', ' ', '\t', '\n', '\r':
			// valid delimiter, continue
		default:
			return body, false
		}
	}

	after = strings.TrimLeft(after, " \t\r\n")
	after = strings.TrimPrefix(after, ",")

	return strings.TrimLeft(after, " \t\r\n"), true
}

// containsBareSpread reports whether s contains the spread token as a
// standalone word outside of a string literal. It is used to produce a
// helpful error when `...` is misplaced.
func containsBareSpread(s string) bool {
	const tok = spreadToken

	var inStr byte

	for i := 0; i+len(tok) <= len(s); i++ {
		c := s[i]

		if inStr != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}

		switch c {
		case '"', '\'', '`':
			inStr = c
			continue
		}

		if s[i:i+len(tok)] != tok {
			continue
		}

		before := i == 0 || !isIdentByte(s[i-1])
		after := i+len(tok) >= len(s) || !isIdentByte(s[i+len(tok)])

		if before && after {
			return true
		}
	}

	return false
}

// buildProjectionEnv prepares the environment that is handed to
// expr.Run. It ensures that the reserved names `__root__` and
// `__state__` are present only when the projection actually needs
// them, so they never appear in the output as an artefact of the
// runtime.
//
// Two views of the input coexist:
//
//	state  — the normalised object form. Any user field whose name
//	         happens to equal a reserved identifier is preserved
//	         verbatim; spread and pass-through therefore never drop
//	         user data.
//
//	env    — the same state, extended (or shadowed) with the reserved
//	         meanings of `__root__` and `__state__`. Inside the
//	         projection, those names always refer to the reserved
//	         values regardless of what the caller's data contains.
//
// When neither reserved name is needed, this is a thin wrapper over
// NormalizeEnv and allocates exactly one map.
func buildProjectionEnv(input any, usesRoot, usesState bool) any {
	if !usesRoot && !usesState {
		return NormalizeEnv(input)
	}

	src, _ := NormalizeEnv(input).(map[string]any)

	state := make(map[string]any, len(src))
	for k, v := range src {
		state[k] = v
	}

	env := make(map[string]any, len(state)+2)
	for k, v := range state {
		env[k] = v
	}
	if usesState {
		env[stateVar] = state
	}
	if usesRoot {
		env[rootVar] = input
	}

	return env
}
