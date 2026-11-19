package compiler

import (
	"strings"
	"unicode/utf8"

	"github.com/nexssp/flow/internal/lexical"
)

type Lexer struct {
	src  string
	pos  int
	line int
}

func NewLexer(src string) *Lexer { return &Lexer{src: src, line: 1} }

func (l *Lexer) Next() Token {
	if token, ok := l.skipSpaceAndComments(); ok {
		return token
	}
	if l.pos >= len(l.src) {
		return Token{Type: TokEOF, Line: l.line, Offset: l.pos, EndOffset: l.pos}
	}
	offset := l.pos
	tok := l.nextToken()
	tok.Offset = offset
	tok.EndOffset = l.pos
	return tok
}

func (l *Lexer) nextToken() Token {
	line := l.line
	c := l.src[l.pos]
	if tok, ok := l.scanOperator(line, c); ok {
		return tok
	}
	if tok, ok := l.scanPunctuation(line, c); ok {
		return tok
	}
	switch c {
	case '@':
		if l.peek() == '{' {
			return l.scanAtBrace()
		}
		return l.scanAtPrompt()
	case '\'', '"', '`':
		return l.scanString(c)
	}
	if isDigit(c) || (c == '-' && isDigit(l.peek())) {
		return l.scanNumber()
	}
	if isIdentStart(c) {
		return l.scanIdent()
	}
	return l.scanUnknown(line)
}

func (l *Lexer) scanPunctuation(line int, c byte) (Token, bool) {
	var tokenType TokenType
	var literal string
	switch c {
	case '&':
		tokenType = TokAmpersand
		literal = "&"
	case '(':
		tokenType = TokLParen
		literal = "("
	case ')':
		tokenType = TokRParen
		literal = ")"
	case '{':
		tokenType = TokLBrace
		literal = "{"
	case '}':
		tokenType = TokRBrace
		literal = "}"
	case '[':
		tokenType = TokLBracket
		literal = "["
	case ']':
		tokenType = TokRBracket
		literal = "]"
	case ':':
		tokenType = TokColon
		literal = ":"
	case '=':
		tokenType = TokEquals
		literal = "="
	case ',':
		tokenType = TokComma
		literal = ","
	case '?':
		tokenType = TokQuestion
		literal = "?"
	case '#':
		tokenType = TokHash
		literal = "#"
	case '~':
		tokenType = TokTilde
		literal = "~"
	default:
		return Token{}, false
	}
	l.pos++
	return Token{Type: tokenType, Lit: literal, Line: line}, true
}

func (l *Lexer) scanUnknown(line int) Token {
	start := l.pos
	if l.src[l.pos] == '!' {
		// ! prefixes strict schema modifiers and remains modifier value text.
		l.pos++
		return Token{Type: TokIdent, Lit: l.src[start:l.pos], Line: line}
	}
	_, size := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += size
	return Token{Type: TokInvalid, Lit: l.src[start:l.pos], Line: line}
}

func (l *Lexer) scanOperator(line int, c byte) (Token, bool) {
	if c == '-' && l.peek() == '>' {
		l.pos += 2
		return Token{Type: TokArrow, Lit: "->", Line: line}, true
	}
	if c == '|' {
		if l.peek() == '|' {
			l.pos += 2
			return Token{Type: TokOrOr, Lit: "||", Line: line}, true
		}
		l.pos++
		return Token{Type: TokPipe, Lit: "|", Line: line}, true
	}
	return Token{}, false
}

func (l *Lexer) scanAtBrace() Token {
	startLine := l.line
	l.pos += 2
	start := l.pos
	depth := 1
	var quote byte
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if quote != 0 {
			quote = l.scanAtBraceQuotedByte(quote)
			continue
		}
		if (c == '"' || c == '\'' || c == '`') && lexical.CanStartQuote(l.src, l.pos, start) {
			quote = c
			l.pos++
			continue
		}
		if l.skipAtBraceComment() {
			continue
		}
		if token, done := l.scanAtBraceDelimiter(start, startLine, &depth); done {
			return token
		}
	}
	return Token{Type: TokAtBrace, Lit: l.src[start:], Line: startLine, Unterminated: true}
}

func (l *Lexer) scanAtBraceQuotedByte(quote byte) byte {
	c := l.src[l.pos]
	if lexical.BackslashEscapesNext(quote, c, l.pos+1 < len(l.src)) {
		if l.src[l.pos+1] == '\n' {
			l.line++
		}
		l.pos += 2
		return quote
	}
	switch c {
	case quote:
		quote = 0
	case '\n':
		l.line++
	}
	l.pos++
	return quote
}

func (l *Lexer) skipAtBraceComment() bool {
	c := l.src[l.pos]
	if c == '/' && l.peek() == '*' {
		l.pos += 2
		for l.pos < len(l.src) {
			if l.src[l.pos] == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
				l.pos += 2
				return true
			}
			if l.src[l.pos] == '\n' {
				l.line++
			}
			l.pos++
		}
		return true
	}
	if (c == '/' && l.peek() == '/') || (c == '#' && l.hashStartsComment()) {
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		return true
	}
	return false
}

func (l *Lexer) scanAtBraceDelimiter(start, startLine int, depth *int) (Token, bool) {
	switch l.src[l.pos] {
	case '{':
		*depth++
	case '}':
		*depth--
		if *depth == 0 {
			content := l.src[start:l.pos]
			l.pos++
			return Token{Type: TokAtBrace, Lit: content, Line: startLine}, true
		}
	case '\n':
		l.line++
	}
	l.pos++
	return Token{}, false
}

func (l *Lexer) scanQuotedAtPromptByte(quote byte) byte {
	c := l.src[l.pos]
	if c == '\\' && l.pos+1 < len(l.src) {
		if l.src[l.pos+1] == '\n' {
			l.line++
		}
		l.pos += 2
		return quote
	}
	if c == quote {
		quote = 0
	}
	if c == '\n' {
		l.line++
	}
	l.pos++
	return quote
}

func (l *Lexer) scanAtPrompt() Token {
	startLine := l.line
	l.pos++
	start := l.pos
	var quote byte
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if quote != 0 {
			quote = l.scanQuotedAtPromptByte(quote)
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			l.pos++
			continue
		}
		if c == '\n' || c == '(' || c == ')' || c == '|' || c == '&' ||
			c == '?' || c == ':' || c == '{' || c == '}' {
			break
		}
		if c == '-' && l.peek() == '>' {
			break
		}
		if c == '@' && l.peek() == '{' {
			break
		}
		l.pos++
	}
	return Token{Type: TokAtPrompt, Lit: l.src[start:l.pos], Line: startLine}
}

func (l *Lexer) skipSpaceAndComments() (Token, bool) {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\r':
			l.pos++
		case '\n':
			l.pos++
			l.line++
		case '#':
			if l.peek() == ' ' || l.peek() == '\t' || l.peek() == '\r' || l.peek() == '\n' || l.isAtLineStart() {
				for l.pos < len(l.src) && l.src[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
			return Token{}, false
		case '/':
			if l.peek() == '/' {
				for l.pos < len(l.src) && l.src[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
			if l.peek() == '*' {
				start, line := l.pos, l.line
				commentEnd := strings.Index(l.src[l.pos+2:], "*/")
				if commentEnd < 0 {
					l.pos = len(l.src)
					return Token{Type: TokInvalid, Lit: l.src[start:], Line: line, Offset: start, EndOffset: l.pos, Unterminated: true}, true
				}
				end := l.pos + 2 + commentEnd + 2
				l.line += strings.Count(l.src[l.pos:end], "\n")
				l.pos = end
				continue
			}
			return Token{}, false
		default:
			return Token{}, false
		}
	}
	return Token{}, false
}

func (l *Lexer) isAtLineStart() bool {
	for i := l.pos - 1; i >= 0; i-- {
		c := l.src[i]
		if c == '\n' {
			return true
		}
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

func (l *Lexer) hashStartsComment() bool {
	if l.pos+1 < len(l.src) {
		next := l.src[l.pos+1]
		if next == ' ' || next == '\t' || next == '\r' || next == '\n' {
			return true
		}
	}
	return l.isAtLineStart()
}

func (l *Lexer) peek() byte {
	if l.pos+1 >= len(l.src) {
		return 0
	}
	return l.src[l.pos+1]
}

func (l *Lexer) scanString(q byte) Token {
	line := l.line
	l.pos++
	start := l.pos
	terminated := false
	for l.pos < len(l.src) && l.src[l.pos] != q {
		if lexical.BackslashEscapesNext(q, l.src[l.pos], l.pos+1 < len(l.src)) {
			if l.src[l.pos+1] == '\n' {
				l.line++
			}
			l.pos += 2
		} else {
			if l.src[l.pos] == '\n' {
				l.line++
			}
			l.pos++
		}
	}
	out := l.src[start:l.pos]
	if l.pos < len(l.src) {
		l.pos++
		terminated = true
	}
	return Token{Type: TokString, Lit: out, Line: line, Unterminated: !terminated}
}

func (l *Lexer) scanNumber() Token {
	line := l.line
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '.' || l.src[l.pos] == '_') {
		l.pos++
	}
	return Token{Type: TokNumber, Lit: l.src[start:l.pos], Line: line}
}

func (l *Lexer) scanIdent() Token {
	line := l.line
	start := l.pos
	for l.pos < len(l.src) && isIdentCont(l.src[l.pos]) {
		if l.src[l.pos] == '-' && l.peek() == '>' {
			break
		}
		l.pos++
	}
	return Token{Type: TokIdent, Lit: l.src[start:l.pos], Line: line}
}
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || c == '.' || c == '/' || c == '~' || c == '*' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func isIdentCont(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '-' }
