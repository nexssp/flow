package core

import "fmt"

type Lexer struct {
	src  string
	pos  int
	line int
}

func NewLexer(src string) *Lexer { return &Lexer{src: src, line: 1} }

func (l *Lexer) Next() Token {
	l.skipSpaceAndComments()
	if l.pos >= len(l.src) {
		return Token{Type: TokEOF, Line: l.line, Offset: l.pos}
	}
	offset := l.pos
	tok := l.nextToken()
	tok.Offset = offset
	return tok
}

func (l *Lexer) nextToken() Token {
	line := l.line
	c := l.src[l.pos]
	if tok, ok := l.scanOperator(line, c); ok {
		return tok
	}
	switch c {
	case '&':
		l.pos++
		return Token{Type: TokAmpersand, Lit: "&", Line: line}
	case '(':
		l.pos++
		return Token{Type: TokLParen, Lit: "(", Line: line}
	case ')':
		l.pos++
		return Token{Type: TokRParen, Lit: ")", Line: line}
	case '{':
		l.pos++
		return Token{Type: TokLBrace, Lit: "{", Line: line}
	case '}':
		l.pos++
		return Token{Type: TokRBrace, Lit: "}", Line: line}
	case ':':
		l.pos++
		return Token{Type: TokColon, Lit: ":", Line: line}
	case '=':
		l.pos++
		return Token{Type: TokEquals, Lit: "=", Line: line}
	case ',':
		l.pos++
		return Token{Type: TokComma, Lit: ",", Line: line}
	case '?':
		l.pos++
		return Token{Type: TokQuestion, Lit: "?", Line: line}
	case '#':
		l.pos++
		return Token{Type: TokHash, Lit: "#", Line: line}
	case '~':
		l.pos++
		return Token{Type: TokTilde, Lit: "~", Line: line}
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
	l.pos++
	return Token{Type: TokIdent, Lit: fmt.Sprintf("%c", c), Line: line}
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
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				content := l.src[start:l.pos]
				l.pos++
				return Token{Type: TokAtBrace, Lit: content, Line: startLine}
			}
		case '\n':
			l.line++
		}
		l.pos++
	}
	return Token{Type: TokAtBrace, Lit: l.src[start:], Line: startLine}
}

func (l *Lexer) scanAtPrompt() Token {
	startLine := l.line
	l.pos++
	start := l.pos
	for l.pos < len(l.src) {
		c := l.src[l.pos]
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

func (l *Lexer) skipSpaceAndComments() {
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
			return
		case '/':
			if l.peek() == '/' {
				for l.pos < len(l.src) && l.src[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
			return
		default:
			return
		}
	}
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
	for l.pos < len(l.src) && l.src[l.pos] != q {
		if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
		} else {
			l.pos++
		}
	}
	out := l.src[start:l.pos]
	if l.pos < len(l.src) {
		l.pos++
	}
	return Token{Type: TokString, Lit: out, Line: line}
}

func (l *Lexer) scanNumber() Token {
	line := l.line
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
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
