package lexical

// CanStartQuote reports whether a quote at position begins a quoted value
// within the source region starting at start.
func CanStartQuote(source string, position, start int) bool {
	for i := position - 1; i >= start; i-- {
		char := source[i]
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			continue
		}
		return char == ':' || char == ',' || char == '[' || char == '{'
	}
	return true
}

// BackslashEscapesNext reports whether the current backslash consumes the
// following byte in a quoted string. Backtick strings are raw.
func BackslashEscapesNext(quote, current byte, hasNext bool) bool {
	return quote != '`' && current == '\\' && hasNext
}
