package compiler

import "strings"

// scanBalancedRaw scans from contentStart through the matching close
// delimiter, returning the raw content between the two (trimmed) and
// leaving the lexer positioned immediately after the close.
//
// The initial nesting depth is 1: the caller has already consumed the
// opening delimiter and is handing us the position right after it.
//
// Quoted strings and nested delimiters are honored, so `}` inside `"…"`,
// `)` inside `"(…)"`, and `[` inside `"[…]"` do not terminate the scan
// early. This is what previously broke projections that contained string
// literals with braces, and `until( x == ")" )`.
//
// On success, call p.resync() to re-seed the two-token lookahead.
func (p *Parser) scanBalancedRaw(
	open, closeDelim byte,
	contentStart, startLine, startCol int,
) (string, error) {
	p.l.pos = contentStart
	p.l.line = startLine
	p.l.col = startCol

	depth := 1
	var inQuote byte

	for p.l.pos < len(p.l.input) {
		ch := p.l.input[p.l.pos]

		if inQuote != 0 {
			if ch == '\\' && p.l.pos+1 < len(p.l.input) {
				p.l.pos += 2
				p.l.col += 2
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			p.l.pos++
			p.l.col++
			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuote = ch
		case open:
			depth++
		case closeDelim:
			depth--
			if depth == 0 {
				raw := strings.TrimSpace(p.l.input[contentStart:p.l.pos])
				p.l.pos++
				p.l.col++
				return raw, nil
			}
		}

		p.l.pos++
		if ch == '\n' {
			p.l.line++
			p.l.col = 1
		} else {
			p.l.col++
		}
	}

	return "", p.errorf("unclosed %q", string(open))
}

// parseProjection parses a `{ ... }` projection block. The body is kept
// raw — it is a small expression language that is interpreted at runtime
// by expr-lang, not re-parsed by this compiler.
func (p *Parser) parseProjection() (Expr, error) {
	if p.tok.Type != TokenLBrace {
		return nil, p.errorf("expected '{'")
	}

	openPos := p.tok.Offset
	openLine := p.tok.Line
	openCol := p.tok.Col

	raw, err := p.scanBalancedRaw('{', '}', openPos+1, openLine, openCol+1)
	if err != nil {
		return nil, err
	}

	p.resync()
	return &ProjectionExpr{Raw: raw}, nil
}

// parseLoop parses `loop( BODY ) until( CONDITION )`. BODY is parsed as a
// normal expression; CONDITION is kept raw because it is an expr-lang
// condition, not a pipeline expression.
func (p *Parser) parseLoop() (Expr, error) {
	p.next() // consume 'loop'

	if !p.expect(TokenLParen) {
		return nil, p.errorf("expected '(' after loop")
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.expect(TokenRParen) {
		return nil, p.errorf("expected ')' after loop body")
	}

	if p.tok.Type != TokenIdent || p.tok.Lit != "until" {
		return nil, p.errorf("expected 'until' after loop body")
	}
	p.next()

	if p.tok.Type != TokenLParen {
		return nil, p.errorf("expected '(' after until")
	}

	openPos := p.tok.Offset
	openLine := p.tok.Line
	openCol := p.tok.Col

	raw, err := p.scanBalancedRaw('(', ')', openPos+1, openLine, openCol+1)
	if err != nil {
		return nil, err
	}

	p.resync()
	return &LoopExpr{Body: body, Until: raw}, nil
}

// parseAssert parses `assert( CONDITION [, "MESSAGE"] )`. The whole
// parenthesised argument list is captured raw and split afterward, so
// commas inside the condition (e.g. function calls) survive.
func (p *Parser) parseAssert() (Expr, error) {
	p.next() // consume 'assert'

	outerLParen := p.tok
	if !p.expect(TokenLParen) {
		return nil, p.errorf("expected '(' after assert")
	}

	raw, err := p.scanBalancedRaw(
		'(', ')',
		outerLParen.Offset+1, outerLParen.Line, outerLParen.Col+1,
	)
	if err != nil {
		return nil, err
	}

	condition, message := splitAssertArguments(raw)
	if condition == "" {
		return nil, p.errorf("assert: condition cannot be empty")
	}

	p.resync()
	return &AssertExpr{Condition: condition, Message: message}, nil
}

// splitAssertArguments splits a raw `cond, "msg"` pair at the first
// top-level comma. Commas inside quotes, brackets, or parentheses do not
// split. When no top-level comma is found, the whole string is the
// condition and the message is empty.
func splitAssertArguments(raw string) (condition, message string) {
	var inQuote byte
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		if inQuote != 0 {
			if character == '\\' && index+1 < len(raw) {
				index++
				continue
			}
			if character == inQuote {
				inQuote = 0
			}
			continue
		}
		switch character {
		case '"', '\'', '`':
			inQuote = character
		case ',':
			condition = strings.TrimSpace(raw[:index])
			rawMessage := strings.TrimSpace(raw[index+1:])
			if len(rawMessage) >= 2 {
				first := rawMessage[0]
				last := rawMessage[len(rawMessage)-1]
				if (first == '"' && last == '"') ||
					(first == '\'' && last == '\'') ||
					(first == '`' && last == '`') {
					rawMessage = rawMessage[1 : len(rawMessage)-1]
				}
			}
			message = rawMessage
			return condition, message
		}
	}
	return strings.TrimSpace(raw), ""
}
