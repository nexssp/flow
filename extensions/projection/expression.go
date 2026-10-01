package projection

import "strings"

// buildProgramSource produces the expr-lang source string from the raw
// projection body:
//
//	{ name: .name }           →  "{ name: name }"
//	{ ..., x: 1 }             →  "__spread__(__root__, { x: 1 })"
//	{ ... }                   →  "__root__"
func buildProgramSource(raw string) string {
	state, rest, hasSpread := splitSpread(raw)
	if !hasSpread {
		return "{ " + preprocessDots(raw) + " }"
	}
	if strings.TrimSpace(rest) == "" {
		return state
	}
	return "__spread__(" + state + ", { " + preprocessDots(rest) + " })"
}

// splitSpread splits `..., body` into a state reference and the rest.
// Returns hasSpread=false for any content that does not start with a
// standalone spread marker.
func splitSpread(raw string) (stateRef, rest string, hasSpread bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "...") {
		return "", raw, false
	}
	after := trimmed[3:]
	if after != "" && after[0] != ',' && after[0] != ' ' && after[0] != '\t' {
		return "", raw, false
	}
	after = strings.TrimLeft(after, " \t\n")
	after = strings.TrimPrefix(after, ",")
	after = strings.TrimLeft(after, " \t\n")
	return "__root__", after, true
}

// preprocessDots rewrites `.field` to `field`, and a bare `.` to
// `__root__`. Fragments inside quotes are copied verbatim; a dot that
// follows an identifier or `)` is left alone (member access).
func preprocessDots(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	i, n := 0, len(src)
	var quote byte

	for i < n {
		c := src[i]

		if quote != 0 {
			out.WriteByte(c)
			if c == '\\' && i+1 < n {
				out.WriteByte(src[i+1])
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}

		switch c {
		case '"', '\'', '`':
			quote = c
			out.WriteByte(c)
			i++
		case '.':
			if i > 0 && isMemberAccess(src[i-1]) {
				out.WriteByte(c)
				i++
				continue
			}
			if i+1 < n && isIdentStart(src[i+1]) {
				i++ // drop the dot; the identifier itself follows
				continue
			}
			out.WriteString("__root__")
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func isMemberAccess(prev byte) bool {
	switch {
	case prev >= 'a' && prev <= 'z',
		prev >= 'A' && prev <= 'Z',
		prev >= '0' && prev <= '9':
		return true
	case prev == '_', prev == ')', prev == ']', prev == '#', prev == '@':
		return true
	}
	return false
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
