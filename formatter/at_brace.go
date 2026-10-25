package formatter

import (
	"strings"

	"github.com/nexssp/flow/compiler"
)

// formatAtBraceBlocks indents multiline argument blocks without rewriting their
// values. In particular, quoted strings are opaque so template delimiters and
// raw multiline string contents remain byte-for-byte unchanged.
func formatAtBraceBlocks(source string, lines []string) []string {
	lineStarts := make([]int, len(lines))
	offset := 0
	for index, line := range lines {
		lineStarts[index] = offset
		offset += len(line) + 1
	}

	lexer := compiler.NewLexer(source)
	for {
		token := lexer.Next()
		if token.Type == compiler.TokEOF {
			return lines
		}
		if token.Type == compiler.TokAtBrace && !token.Unterminated && strings.Contains(token.Lit, "\n") {
			reindentArgumentBlock(source, lines, lineStarts, token)
		}
	}
}

func reindentArgumentBlock(source string, lines []string, lineStarts []int, token compiler.Token) {
	bodyStart := token.Offset + 2
	closeOffset := bodyStart + len(token.Lit)
	if closeOffset >= len(source) || source[closeOffset] != '}' {
		return
	}
	firstLine := strings.Count(source[:token.Offset], "\n")
	lastLine := strings.Count(source[:closeOffset], "\n")
	if firstLine >= len(lines) || lastLine >= len(lines) {
		return
	}
	baseIndent := leadingWhitespace(source[lineStarts[firstLine]:token.Offset])
	reindentArgumentBlockLines(source, lines, lineStarts, firstLine, lastLine, bodyStart, closeOffset, baseIndent)
}

func reindentArgumentBlockLines(source string, lines []string, lineStarts []int, firstLine, lastLine, bodyStart, closeOffset int, baseIndent string) {
	depth := 1
	var quote byte
	for lineIndex := firstLine; lineIndex <= lastLine; lineIndex++ {
		lineStart := lineStarts[lineIndex]
		if lineIndex > firstLine {
			reindentArgumentBlockLine(lines, lineIndex, depth, quote, baseIndent)
		}
		lineEnd := sourceLineEnd(source, lineStart)
		scanStart := max(lineStart, bodyStart)
		scanEnd := min(lineEnd, closeOffset+1)
		depth, quote = scanArgumentBlockLine(source, scanStart, scanEnd, bodyStart, depth, quote)
	}
}

func reindentArgumentBlockLine(lines []string, index, depth int, quote byte, baseIndent string) {
	if quote != 0 {
		return
	}
	trimmed := strings.TrimLeft(lines[index], " \t")
	if strings.TrimSpace(trimmed) == "" {
		lines[index] = ""
		return
	}
	indentDepth := depth
	for strings.HasPrefix(trimmed, "}") {
		indentDepth--
		trimmed = strings.TrimLeft(trimmed[1:], " \t")
	}
	if indentDepth < 0 {
		indentDepth = 0
	}
	lines[index] = baseIndent + strings.Repeat(" ", indentDepth*2) + strings.TrimLeft(lines[index], " \t")
}

func scanArgumentBlockLine(source string, start, end, blockStart, depth int, quote byte) (nextDepth int, nextQuote byte) {
	for position := start; position < end; position++ {
		char := source[position]
		if quote != 0 {
			if char == '\\' && position+1 < end {
				position++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if isArgumentQuote(char) && argumentQuoteCanStart(source, position, blockStart) {
			quote = char
			continue
		}
		switch char {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth, quote
}

func sourceLineEnd(source string, lineStart int) int {
	lineEnd := strings.IndexByte(source[lineStart:], '\n')
	if lineEnd < 0 {
		return len(source)
	}
	return lineStart + lineEnd
}

func isArgumentQuote(char byte) bool {
	return char == '"' || char == '\'' || char == '`'
}

func argumentQuoteCanStart(source string, position, start int) bool {
	for index := position - 1; index >= start; index-- {
		char := source[index]
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			continue
		}
		return char == ':' || char == ',' || char == '[' || char == '{'
	}
	return true
}
