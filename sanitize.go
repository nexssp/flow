package flow

import "strings"

// SanitizeDSL converts a full .flow manifest into the line-oriented
// pipeline the flow compiler consumes.
//
// One statement per line. Comments (# or //), config directives
// (@config:, @assert:, @require, …), and route declaration headers
// (unindented lines that bind :route= or :http= and contain no
// pipeline arrow) are dropped. Everything else is preserved verbatim
// so the lexer can terminate an unquoted @prompt annotation at the
// end of its line.
func SanitizeDSL(rawContent string) string {
	// Strip UTF-8 BOM
	rawContent = strings.TrimPrefix(rawContent, "\xef\xbb\xbf")

	var sb strings.Builder
	sb.Grow(len(rawContent))

	for rawContent != "" {
		var line string
		if i := strings.IndexByte(rawContent, '\n'); i >= 0 {
			line, rawContent = rawContent[:i], rawContent[i+1:]
		} else {
			line, rawContent = rawContent, ""
		}

		line = strings.TrimRight(line, "\r")

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		c := trimmed[0]
		if c == '#' || c == '@' || (len(trimmed) >= 2 && trimmed[0] == '/' && trimmed[1] == '/') {
			continue
		}

		// Route declaration header: an unindented line that binds a
		// route (":route=" or ":http=") and carries no pipeline arrow.
		// This is the name under which the flow is mounted, not a step
		// the compiler should try to execute. Indented lines are step
		// atoms and are never treated as headers, even if they carry
		// route modifiers.
		isIndented := line != "" && (line[0] == ' ' || line[0] == '\t')
		if !isIndented && !strings.Contains(trimmed, "->") &&
			(strings.Contains(trimmed, ":route=") || strings.Contains(trimmed, ":http=")) {
			continue
		}

		sb.WriteString(trimmed)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// stripAtAnnotations removes a trailing inline @-annotation from a DSL
// line. Quoted '@' characters (with \ escapes inside quotes) are
// preserved. Kept for callers that need the pre-sanitize form; the
// SanitizeDSL above preserves annotations instead, because the lexer
// now consumes them as TokenAtPrompt.
func stripAtAnnotations(line string) string {
	inQuotes := false

	var quoteCh byte

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if inQuotes {
			if ch == '\\' && i+1 < len(line) {
				i++

				continue
			}

			if ch == quoteCh {
				inQuotes = false
			}

			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuotes = true
			quoteCh = ch
		case '@':
			return strings.TrimSpace(line[:i])
		}
	}

	return line
}
