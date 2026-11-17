package compiler

import "strings"

func appendExprComment(raw string, index int, output *strings.Builder) int {
	commentStart := index
	for index < len(raw) && raw[index] != '\n' && raw[index] != '\r' {
		index++
	}
	for commentIndex := commentStart; commentIndex < index; commentIndex++ {
		if raw[commentIndex] == '\t' {
			output.WriteByte('\t')
		} else {
			output.WriteByte(' ')
		}
	}
	for index < len(raw) && (raw[index] == '\r' || raw[index] == '\n') {
		output.WriteByte(raw[index])
		index++
	}
	return index
}

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
			if c == '\\' && quote != '`' && i+1 < len(raw) {
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
			i = appendExprComment(raw, i, &sb)
			continue
		default:
			sb.WriteByte(c)
		}
		i++
	}
	return sb.String()
}
