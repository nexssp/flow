package flow

import "strings"

// SanitizeDSL strips comments, directives, and declaration headers
// from a .nflow source so that only the executable pipeline remains.
func SanitizeDSL(rawContent string) string {
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
			sb.WriteByte('\n')
			continue
		}

		c := trimmed[0]
		// Skip comments and directives (except inline args @{)
		if c == '#' || (c == '@' && !strings.HasPrefix(trimmed, "@{")) || (len(trimmed) >= 2 && trimmed[0] == '/' && trimmed[1] == '/') {
			sb.WriteByte('\n')
			continue
		}

		isIndented := line != "" && (line[0] == ' ' || line[0] == '\t')
		hasArrow := strings.Contains(trimmed, "->")
		hasAnnotation := strings.Contains(trimmed, "@")
		hasBrace := strings.Contains(trimmed, "{") || strings.Contains(trimmed, "}")
		hasRouteModifier := strings.Contains(trimmed, ":route=") || strings.Contains(trimmed, ":http=")

		// Only strip legacy top-level action headers if they don't contain pipeline operators or braces
		if !isIndented && !hasArrow && !hasAnnotation && !hasBrace && hasRouteModifier {
			sb.WriteByte('\n')
			continue
		}

		sb.WriteString(trimmed)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// stripAtAnnotations removes everything from the first unquoted '@' on
// a line. Retained for callers that need to normalise a single atom.
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
