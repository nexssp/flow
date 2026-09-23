package nodes

import (
	"reflect"
	"strings"
)

// PreprocessDotNotation rewrites a projection expression so that the
// leading-dot shorthand can be used with flattened state maps.
//
// # Rewrites
//
//	.foo              →  foo                     (identifier shorthand)
//	.foo.bar          →  foo.bar
//	.foo[0]           →  foo[0]
//	.foo.bar[2].baz   →  foo.bar[2].baz
//
// The leading-dot shorthand is *not* rewritten when:
//
//   - the dot is part of a floating-point literal (".5", "3.14"),
//   - the dot follows an identifier, ")", or "]", because in that case
//     it is an ordinary member access on an existing value, and
//   - the dot follows a predicate scope marker `#`, which is the
//     modern expr-lang syntax for referring to the current element of
//     a built-in array function such as `sortBy`, `groupBy`, `map`,
//     `filter`, or `reduce`.
//
// In the last case the dot must be preserved, otherwise the predicate
// body would be corrupted (`#.Age` must not become `#Age`).
//
// # Root reference
//
// When the expression contains a bare `.` that is not followed by an
// identifier, the dot is replaced with the reserved identifier
// `__root__` and `usesRoot` is set to true. This allows a projection
// to reference the whole previous state as `__root__` explicitly.
//
// The function is intentionally lexical: it does not parse the
// expression, it only classifies dots by their immediate neighbours.
// This is sufficient for the projections produced by the DSL and keeps
// the hot path allocation-free apart from the builder itself.
func PreprocessDotNotation(src string) (out string, usesRoot bool) {
	var sb strings.Builder
	sb.Grow(len(src) + 16)

	var (
		inQuote byte
		escaped bool
	)

	n := len(src)

	for i := 0; i < n; i++ {
		ch := src[i]

		if inQuote != 0 {
			sb.WriteByte(ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == inQuote:
				inQuote = 0
			}
			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuote = ch
			sb.WriteByte(ch)
			continue
		}

		if ch != '.' {
			sb.WriteByte(ch)
			continue
		}

		// ── Float literal guards ───────────────────────────────────────
		// 3.14 → keep the dot. .5 → keep the dot.
		if i > 0 && isASCIIDigit(src[i-1]) {
			sb.WriteByte(ch)
			continue
		}
		if i+1 < n && isASCIIDigit(src[i+1]) {
			sb.WriteByte(ch)
			continue
		}

		// ── Predicate scope marker ─────────────────────────────────────
		// `#.field` inside sortBy/groupBy/map/filter/reduce refers to
		// the current element. The dot is part of the syntax and must
		// survive unchanged.
		if i > 0 && src[i-1] == '#' {
			sb.WriteByte(ch)
			continue
		}

		// ── Member access on an existing value ─────────────────────────
		// a.b, f().c, arr[0].d — the dot belongs to the receiver, not
		// to the leading-dot shorthand.
		if i > 0 {
			prev := src[i-1]
			if isIdentByte(prev) || prev == ')' || prev == ']' {
				sb.WriteByte(ch)
				continue
			}
		}

		// ── Leading-dot shorthand ──────────────────────────────────────
		// `.foo` is rewritten to `foo`, since the environment is
		// flattened by NormalizeEnv.
		if i+1 < n && isIdentStartByte(src[i+1]) {
			continue
		}

		// ── Bare root reference ────────────────────────────────────────
		// A free-standing `.` becomes `__root__`.
		sb.WriteString("__root__")
		usesRoot = true
	}

	return sb.String(), usesRoot
}

// NormalizeEnv converts a nested value into a flat lookup map that can
// be handed to expr-lang as its environment.
//
// # Behavior
//
//   - Maps are copied. Every key is exposed three ways: verbatim, in
//     lower-case, and — when the key itself contains dots — also as a
//     nested path. `{"user.name": "Alice"}` therefore yields both
//     `user.name` (literal key) and `user.name` reachable as
//     `user["name"]` or `user.name` after PreprocessDotNotation.
//
//   - Structs are flattened by their exported field names, their
//     JSON tags, and their lower-cased Go names. This lets expressions
//     use any of the three spellings interchangeably.
//
//   - Pointers and interfaces are dereferenced. Nil values become nil.
//
//   - All other values are returned unchanged.
//
// # Cost
//
// One map allocation per nested map or struct. Small, predictable, and
// bounded by the shape of the input.
func NormalizeEnv(input any) any {
	if input == nil {
		return nil
	}

	if m, ok := input.(map[string]any); ok {
		out := make(map[string]any, len(m)*3)
		for k, v := range m {
			normalizedVal := NormalizeEnv(v)
			out[k] = normalizedVal
			out[strings.ToLower(k)] = normalizedVal

			if strings.ContainsRune(k, '.') {
				parts := strings.Split(k, ".")
				curr := out
				for i := 0; i < len(parts)-1; i++ {
					sub, exists := curr[parts[i]]
					if !exists {
						subMap := make(map[string]any)
						curr[parts[i]] = subMap
						curr = subMap
					} else if subMap, ok := sub.(map[string]any); ok {
						curr = subMap
					}
				}
				curr[parts[len(parts)-1]] = normalizedVal
			}
		}
		return out
	}

	rv := reflect.ValueOf(input)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return input
	}

	typ := rv.Type()
	out := make(map[string]any, typ.NumField()*3)

	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		val := rv.Field(i).Interface()
		normalizedVal := NormalizeEnv(val)

		out[field.Name] = normalizedVal

		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				out[parts[0]] = normalizedVal
			}
		}

		out[strings.ToLower(field.Name)] = normalizedVal
	}

	return out
}

func isIdentStartByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentByte(b byte) bool {
	return isIdentStartByte(b) || isASCIIDigit(b)
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
