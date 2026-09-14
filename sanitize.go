package flow

import "strings"

// SanitizeDSL converts a full .flow manifest into the single-line arrow
// pipeline that the flow compiler consumes.
func SanitizeDSL(rawContent string) string {
	var sb strings.Builder

	sb.Grow(len(rawContent)) // single allocation; worst case == input length

	needSpace := false

	for rawContent != "" {
		var line string

		if i := strings.IndexByte(rawContent, '\n'); i >= 0 {
			line, rawContent = rawContent[:i], rawContent[i+1:]
		} else {
			line, rawContent = rawContent, ""
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		c := trimmed[0]
		if c == '#' || c == '@' ||
			(len(trimmed) >= 2 && trimmed[0] == '/' && trimmed[1] == '/') {
			continue
		}

		isIndented := line != "" && (line[0] == ' ' || line[0] == '\t')
		if !isIndented &&
			!strings.Contains(trimmed, "->") &&
			(strings.Contains(trimmed, ":route=") || strings.Contains(trimmed, ":http=")) {
			continue
		}

		trimmed = stripAtAnnotations(trimmed)
		if trimmed == "" {
			continue
		}

		if needSpace {
			sb.WriteByte(' ')
		}

		sb.WriteString(trimmed)

		needSpace = true
	}

	return sb.String()
}

// stripAtAnnotations removes a trailing inline @-annotation from a DSL line.
// Quoted '@' characters (with `\` escapes inside quotes) are preserved.
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
			inQuotes, quoteCh = true, ch
		case '@':
			return strings.TrimSpace(line[:i])
		}
	}

	return line
}
