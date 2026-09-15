package flow

import "strings"

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

		// Skip config and comment lines completely
		c := trimmed[0]
		if c == '#' || c == '@' || (len(trimmed) >= 2 && trimmed[0] == '/' && trimmed[1] == '/') {
			continue
		}

		// Preserve newlines so the lexer can safely terminate unquoted @ prompts
		sb.WriteString(trimmed)
		sb.WriteByte('\n')
	}

	return sb.String()
}

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
