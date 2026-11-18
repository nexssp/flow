package formatter

import (
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/internal/lexical"
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

type parenthesizedArgumentSpan struct {
	openIndex  int
	closeIndex int
}

func parenthesizedArgumentSpans(source string) ([]compiler.Token, []parenthesizedArgumentSpan) {
	lexer := compiler.NewLexer(source)
	var tokens []compiler.Token
	for {
		token := lexer.Next()
		tokens = append(tokens, token)
		if token.Type == compiler.TokEOF {
			break
		}
	}

	var spans []parenthesizedArgumentSpan
	for index := 0; index < len(tokens); {
		if tokens[index].Type != compiler.TokLParen || !startsNamedArgumentList(source, tokens, index) {
			index++
			continue
		}
		depth := 1
		closeIndex := -1
		for next := index + 1; next < len(tokens); next++ {
			switch tokens[next].Type {
			case compiler.TokLParen:
				depth++
			case compiler.TokRParen:
				depth--
				if depth == 0 {
					closeIndex = next
				}
			case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
				compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
				compiler.TokLBrace, compiler.TokRBrace, compiler.TokLBracket, compiler.TokRBracket,
				compiler.TokColon, compiler.TokEquals, compiler.TokComma, compiler.TokQuestion,
				compiler.TokHash, compiler.TokTilde, compiler.TokAtBrace, compiler.TokAtPrompt, compiler.TokInvalid:
				// Non-parenthesis tokens do not affect the nesting depth.
			}
			if closeIndex >= 0 {
				break
			}
		}
		if closeIndex < 0 {
			index++
			continue
		}
		spans = append(spans, parenthesizedArgumentSpan{openIndex: index, closeIndex: closeIndex})
		index = closeIndex + 1
	}
	return tokens, spans
}

func startsNamedArgumentList(source string, tokens []compiler.Token, openIndex int) bool {
	if openIndex+1 >= len(tokens) {
		return false
	}
	first := tokens[openIndex+1]
	if first.Type == compiler.TokIdent && openIndex+2 < len(tokens) {
		next := tokens[openIndex+2].Type
		if next == compiler.TokColon || next == compiler.TokComma {
			return true
		}
	}
	return strings.HasPrefix(strings.TrimSpace(source[tokens[openIndex].EndOffset:]), "...")
}

func formatParenthesizedArgumentLists(source string, lines []string, opaqueLines map[int]bool) []string {
	lineStarts := make([]int, len(lines))
	offset := 0
	for index, line := range lines {
		lineStarts[index] = offset
		offset += len(line) + 1
	}
	tokens, spans := parenthesizedArgumentSpans(source)
	for _, span := range spans {
		openToken, closeToken := tokens[span.openIndex], tokens[span.closeIndex]
		if closeToken.Line <= openToken.Line {
			continue
		}
		reindentParenthesizedArgumentList(source, lines, lineStarts, tokens, span, opaqueLines)
	}
	return lines
}

func reindentParenthesizedArgumentList(source string, lines []string, lineStarts []int, tokens []compiler.Token, span parenthesizedArgumentSpan, opaqueLines map[int]bool) {
	open := tokens[span.openIndex]
	firstLine := open.Line - 1
	lastLine := tokens[span.closeIndex].Line - 1
	if firstLine < 0 || lastLine >= len(lines) || firstLine >= len(lineStarts) {
		return
	}
	baseIndent := leadingWhitespace(source[lineStarts[firstLine]:open.Offset])
	depth := 1
	tokenIndex := span.openIndex + 1
	for tokenIndex < span.closeIndex && tokens[tokenIndex].Line <= open.Line {
		depth = adjustDelimiterDepth(depth, tokens[tokenIndex].Type)
		tokenIndex++
	}
	for lineIndex := firstLine + 1; lineIndex <= lastLine; lineIndex++ {
		if !opaqueLines[lineIndex] {
			trimmed := strings.TrimLeft(lines[lineIndex], " \t")
			if strings.TrimSpace(trimmed) == "" {
				lines[lineIndex] = ""
			} else {
				indentDepth := depth - leadingClosingDelimiterCount(trimmed)
				indentDepth = max(indentDepth, 0)
				lines[lineIndex] = baseIndent + strings.Repeat(" ", indentDepth*2) + trimmed
			}
		}
		lineNumber := lineIndex + 1
		for tokenIndex <= span.closeIndex && tokens[tokenIndex].Line <= lineNumber {
			depth = adjustDelimiterDepth(depth, tokens[tokenIndex].Type)
			tokenIndex++
		}
	}
}

func leadingClosingDelimiterCount(line string) int {
	line = strings.TrimLeft(line, " \t")
	count := 0
	for line != "" {
		switch line[0] {
		case ')', '}', ']':
			count++
			line = strings.TrimLeft(line[1:], " \t")
		default:
			return count
		}
	}
	return count
}

func adjustDelimiterDepth(depth int, token compiler.TokenType) int {
	switch token {
	case compiler.TokLParen, compiler.TokLBrace, compiler.TokLBracket:
		return depth + 1
	case compiler.TokRParen, compiler.TokRBrace, compiler.TokRBracket:
		return depth - 1
	case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
		compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
		compiler.TokColon, compiler.TokEquals, compiler.TokComma, compiler.TokQuestion,
		compiler.TokHash, compiler.TokTilde, compiler.TokAtBrace, compiler.TokAtPrompt, compiler.TokInvalid:
		return depth
	}
	return depth
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
		if isArgumentQuote(char) && lexical.CanStartQuote(source, position, blockStart) {
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
