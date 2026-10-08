package cli

import "strings"

// colorizeJSONLine colors one line of pretty-printed JSON. Keys (strings
// followed by `:`) in cyan, string values in green, numbers in yellow,
// true/false/null in magenta, punctuation in dim.
func colorizeJSONLine(line string, useColor bool) string {
	if !useColor || line == "" {
		return line
	}
	var b strings.Builder
	b.Grow(len(line) + 16)

	i, n := 0, len(line)
	for i < n {
		i = colorizeJSONToken(&b, line, i, n)
	}
	return b.String()
}

// colorizeJSONToken writes the token at position i and returns the next
// unconsumed index. One token per call keeps the orchestrator under the
// cyclomatic-complexity budget.
func colorizeJSONToken(b *strings.Builder, line string, i, n int) int {
	c := line[i]
	switch {
	case c == ' ' || c == '\t':
		b.WriteByte(c)
		return i + 1
	case c == '"':
		return colorizeJSONString(b, line, i, n)
	case c == '-' || (c >= '0' && c <= '9'):
		return colorizeJSONNumber(b, line, i, n)
	default:
		return colorizeJSONLiteral(b, line, i)
	}
}

// colorizeJSONString writes a JSON string literal, choosing cyan for a
// key (a string followed by `:`) and green for a value.
func colorizeJSONString(b *strings.Builder, line string, i, n int) int {
	end := scanJSONStringEnd(line, i, n)
	if end < 0 {
		// Unterminated quote; write the remainder unchanged so the
		// caller's output stays aligned with the input.
		b.WriteString(line[i:])
		return n
	}
	raw := line[i : end+1]
	after := skipJSONWhitespace(line, end+1, n)
	if after < n && line[after] == ':' {
		b.WriteString(ansiCyan + raw + ansiReset)
	} else {
		b.WriteString(ansiGreen + raw + ansiReset)
	}
	return end + 1
}

// scanJSONStringEnd returns the index of the closing quote of the string
// starting at open (which must be a quote), or -1 if the string never
// closes. Backslash escapes are honored.
func scanJSONStringEnd(line string, open, n int) int {
	j := open + 1
	for j < n {
		switch line[j] {
		case '\\':
			j += 2
			continue
		case '"':
			return j
		}
		j++
	}
	return -1
}

// skipJSONWhitespace returns the next non-blank index at or after i.
func skipJSONWhitespace(line string, i, n int) int {
	for i < n && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i
}

// colorizeJSONNumber writes a numeric token (int, float, exponent) in
// yellow and returns the next unconsumed index.
func colorizeJSONNumber(b *strings.Builder, line string, i, n int) int {
	j := i
	if line[j] == '-' {
		j++
	}
	for j < n && isJSONNumberByte(line[j]) {
		j++
	}
	b.WriteString(ansiYellow + line[i:j] + ansiReset)
	return j
}

func isJSONNumberByte(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-':
		return true
	}
	return false
}

// colorizeJSONLiteral writes `true`, `false`, `null` in magenta and any
// other byte in dim. The literal check runs before the fallback so
// "trueish" is not colorized as a keyword.
//
// The byte-count parameter n is intentionally absent: this function
// inspects at most len(lit) bytes ahead of i, and the caller has
// already guaranteed i < len(line). Passing n would be dead weight.
func colorizeJSONLiteral(b *strings.Builder, line string, i int) int {
	for _, lit := range [...]string{"true", "false", "null"} {
		if strings.HasPrefix(line[i:], lit) {
			b.WriteString(ansiMagenta + lit + ansiReset)
			return i + len(lit)
		}
	}
	b.WriteString(ansiDim)
	b.WriteByte(line[i])
	b.WriteString(ansiReset)
	return i + 1
}
