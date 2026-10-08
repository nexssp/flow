package core

import "strings"

// StripExprComments removes `#` comments from an expression that will be
// handed to expr-lang.
//
// `#` is depth-aware: inside parentheses `#` is expr-lang's iterator
// placeholder — filter(# > 0), all(items, # != "x"), map(#.name) — and
// survives verbatim. Only at parenthesis depth 0 does `#` begin a
// comment, because that is the only position where a bare `#` would be
// syntactically invalid in expr-lang.
//
// Line terminators (\n, \r\n) are preserved so any error expr-lang
// reports keeps its line number aligned with the source text the
// expression was cut from. That alignment is what wrapMacroError and
// the projection diagnostic rely on.
//
// Every path that hands user-authored text to expr-lang goes through
// this function: @assert, assert(), loop() until, match() conditions,
// @on_error conditions and targets, and projection bodies.
func StripExprComments(raw string) string {
	if !strings.Contains(raw, "#") {
		return raw
	}
	var sb strings.Builder
	sb.Grow(len(raw))

	var quote byte
	depth := 0
	i := 0
	for i < len(raw) {
		c := raw[i]

		if quote != 0 {
			sb.WriteByte(c)
			if c == '\\' && i+1 < len(raw) {
				sb.WriteByte(raw[i+1])
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
			sb.WriteByte(c)
		case '(':
			depth++
			sb.WriteByte(c)
		case ')':
			if depth > 0 {
				depth--
			}
			sb.WriteByte(c)
		case '#':
			if depth > 0 {
				// Iterator placeholder — expr-lang grammar, not a comment.
				sb.WriteByte(c)
				i++
				continue
			}
			// Depth-0 comment: skip to end of line, then carry the line
			// terminator verbatim so CRLF files stay CRLF.
			for i < len(raw) && raw[i] != '\n' && raw[i] != '\r' {
				i++
			}
			for i < len(raw) && (raw[i] == '\r' || raw[i] == '\n') {
				sb.WriteByte(raw[i])
				i++
			}
			continue
		default:
			sb.WriteByte(c)
		}
		i++
	}
	return sb.String()
}
