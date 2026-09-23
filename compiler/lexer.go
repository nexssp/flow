package compiler

import (
	"strconv"
	"strings"
	"unicode"
)

type Lexer struct {
	input string
	pos   int
	line  int
	col   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: input, line: 1, col: 1}
}

func (l *Lexer) Next() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Line: l.line, Col: l.col, Offset: l.pos}
	}

	offset := l.pos
	ch := l.input[l.pos]

	switch {
	case ch == '(':
		l.pos++
		l.col++
		return Token{Type: TokenLParen, Lit: "(", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == ')':
		l.pos++
		l.col++
		return Token{Type: TokenRParen, Lit: ")", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '{':
		l.pos++
		l.col++
		return Token{Type: TokenLBrace, Lit: "{", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '}':
		l.pos++
		l.col++
		return Token{Type: TokenRBrace, Lit: "}", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '?':
		l.pos++
		l.col++
		return Token{Type: TokenQuestion, Lit: "?", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == ':':
		l.pos++
		l.col++
		return Token{Type: TokenColon, Lit: ":", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '=':
		l.pos++
		l.col++
		return Token{Type: TokenAssign, Lit: "=", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '@':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '{' {
			return l.scanAtBrace()
		}
		return l.scanAtPrompt()
	case ch == '#':
		l.pos++
		l.col++
		return Token{Type: TokenHash, Lit: "#", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '~':
		l.pos++
		l.col++
		return Token{Type: TokenTilde, Lit: "~", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == ',':
		l.pos++
		l.col++
		return Token{Type: TokenComma, Lit: ",", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '&':
		l.pos++
		l.col++
		return Token{Type: TokenAmpersand, Lit: "&", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '|':
		l.pos++
		l.col++
		if l.pos < len(l.input) && l.input[l.pos] == '|' {
			l.pos++
			l.col++
			return Token{Type: TokenOr, Lit: "||", Line: l.line, Col: l.col - 2, Offset: offset}
		}
		return Token{Type: TokenPipe, Lit: "|", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '-':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '>' {
			l.pos += 2
			l.col += 2
			return Token{Type: TokenArrow, Lit: "->", Line: l.line, Col: l.col - 2, Offset: offset}
		}
		if l.pos+1 < len(l.input) && unicode.IsDigit(rune(l.input[l.pos+1])) {
			return l.scanNumber()
		}
		l.pos++
		l.col++
		return Token{Type: TokenInvalid, Lit: "-", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == '"' || ch == '\'':
		return l.scanString(ch)
	case unicode.IsDigit(rune(ch)):
		return l.scanNumber()
	case ch == '<' || ch == '>' || ch == '!':
		l.pos++
		l.col++
		// These characters are legal inside parenthesized, braced, or
		// "until" expressions. The parser scans those regions directly from
		// the raw input (see parseAssert, parseProjection, parseLoop), but
		// the one-token lookahead in Parser.next eagerly lexes the first
		// character after the opening delimiter. Returning a benign token
		// here keeps that eager peek from producing TokenInvalid and
		// poisoning Parser.err, which would otherwise abort every
		// subsequent next() call.
		return Token{
			Type:   TokenIdent,
			Lit:    string(ch),
			Line:   l.line,
			Col:    l.col - 1,
			Offset: offset,
		}
	case ch == '[':
		l.pos++
		l.col++
		return Token{Type: TokenLBracket, Lit: "[", Line: l.line, Col: l.col - 1, Offset: offset}
	case ch == ']':
		l.pos++
		l.col++
		return Token{Type: TokenRBracket, Lit: "]", Line: l.line, Col: l.col - 1, Offset: offset}
	default:
		if unicode.IsLetter(rune(ch)) || ch == '_' || ch == '.' {
			return l.scanIdent()
		}
		l.pos++
		l.col++
		return Token{Type: TokenInvalid, Lit: string(ch), Line: l.line, Col: l.col - 1, Offset: offset}
	}
}

// scanAtBrace reads `@{ ... }` and returns the raw contents (without
// the outer @{ and }). Nested braces are tracked so the closing brace
// is the matching one. Newlines inside the block are permitted.
func (l *Lexer) scanAtBrace() Token {
	startLine, startCol := l.line, l.col
	offset := l.pos
	l.pos += 2
	l.col += 2

	contentStart := l.pos
	depth := 1

	for l.pos < len(l.input) {
		c := l.input[l.pos]
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				content := l.input[contentStart:l.pos]
				l.pos++
				l.col++
				return Token{
					Type:   TokenAtBrace,
					Lit:    strings.TrimSpace(content),
					Line:   startLine,
					Col:    startCol,
					Offset: offset,
				}
			}
		case '\n':
			l.line++
			l.col = 1
			l.pos++
			continue
		}
		l.pos++
		l.col++
	}

	return Token{
		Type:   TokenInvalid,
		Lit:    l.input[contentStart:],
		Line:   startLine,
		Col:    startCol,
		Offset: offset,
	}
}

// scanAtPrompt reads `@text` up to the next structural delimiter.
//
// The prompt ends at a newline, at a grouping or operator character,
// or immediately before a following `@{...}` block. That last case is
// what allows `agent.x @security @{ goal: .ticket }`: the free-form
// prompt "security" is captured, and the brace block is lexed
// separately by the next Next() call.
func (l *Lexer) scanAtPrompt() Token {
	start := l.pos
	offset := l.pos
	l.pos++
	l.col++

	for l.pos < len(l.input) {
		c := l.input[l.pos]

		if c == '\n' || c == '(' || c == ')' ||
			c == '|' || c == '&' || c == '?' || c == ':' {
			break
		}
		if c == '-' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '>' {
			break
		}
		// A following @{...} block terminates the prompt; the brace
		// token is consumed by the next call to Next.
		if c == '@' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '{' {
			break
		}
		l.pos++
		l.col++
	}

	lit := strings.TrimSpace(l.input[start+1 : l.pos])
	return Token{Type: TokenAtPrompt, Lit: lit, Line: l.line, Col: l.col - len(lit), Offset: offset}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		switch ch {
		case ' ', '\t', '\r':
			l.pos++
			l.col++
		case '\n':
			l.pos++
			l.line++
			l.col = 1
		default:
			return
		}
	}
}

func (l *Lexer) scanString(quote byte) Token {
	start := l.pos
	offset := l.pos
	l.pos++
	l.col++

	for l.pos < len(l.input) && l.input[l.pos] != quote {
		if l.input[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}

	if l.pos >= len(l.input) {
		return Token{
			Type:   TokenInvalid,
			Lit:    l.input[start:],
			Line:   l.line,
			Col:    l.col - len(l.input[start:]),
			Offset: offset,
		}
	}

	l.pos++
	l.col++

	return Token{
		Type:   TokenString,
		Lit:    l.input[start+1 : l.pos-1],
		Line:   l.line,
		Col:    l.col - len(l.input[start:l.pos]),
		Offset: offset,
	}
}

func (l *Lexer) scanNumber() Token {
	start := l.pos
	offset := l.pos

	if l.pos < len(l.input) && l.input[l.pos] == '-' {
		l.pos++
		l.col++
	}
	for l.pos < len(l.input) && (unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '.') {
		l.pos++
		l.col++
	}

	lit := l.input[start:l.pos]
	if _, err := strconv.ParseFloat(lit, 64); err != nil {
		return Token{Type: TokenInvalid, Lit: lit, Line: l.line, Col: l.col - len(lit), Offset: offset}
	}
	return Token{Type: TokenNumber, Lit: lit, Line: l.line, Col: l.col - len(lit), Offset: offset}
}

func (l *Lexer) scanIdent() Token {
	start := l.pos
	offset := l.pos

	for l.pos < len(l.input) {
		if l.input[l.pos] == '-' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '>' {
			break
		}
		r := rune(l.input[l.pos])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-' || r == '/' {
			l.pos++
			l.col++
		} else {
			break
		}
	}

	lit := l.input[start:l.pos]
	return Token{Type: TokenIdent, Lit: lit, Line: l.line, Col: l.col - len(lit), Offset: offset}
}
