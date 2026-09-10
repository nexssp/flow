package compiler

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/xerr"
)

type Parser struct {
	l    *Lexer
	tok  Token
	peek Token
	err  error
}

func NewParser(input string) *Parser {
	p := &Parser{l: NewLexer(input)}
	p.next()
	p.next()

	return p
}

func (p *Parser) next() {
	if p.err != nil {
		return
	}

	p.tok = p.peek

	p.peek = p.l.Next()
	if p.peek.Type == TokenInvalid {
		p.err = p.errorf("invalid character: %q", p.peek.Lit)
	}
}

func (p *Parser) expect(tt TokenType) bool {
	if p.tok.Type == tt {
		p.next()

		return true
	}

	p.err = p.errorf("expected %s, got %s", tt, p.tok.Type)

	return false
}

func (p *Parser) errorf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)

	return xerr.BadRequest(fmt.Sprintf("parse error at line %d, col %d: %s", p.tok.Line, p.tok.Col, msg))
}

func (p *Parser) ParseExpression() (Expr, error) {
	if p.err != nil {
		return nil, p.err
	}

	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	if p.tok.Type != TokenEOF {
		return nil, p.errorf("unexpected token %q after expression", p.tok.Lit)
	}

	return expr, nil
}

func (p *Parser) parseExpr() (Expr, error) {
	return p.parseConditional()
}

// parseConditional handles '?'
func (p *Parser) parseConditional() (Expr, error) {
	gate, err := p.parseFallback()
	if err != nil {
		return nil, err
	}

	if p.tok.Type == TokenQuestion {
		p.next()

		target, err := p.parseFallback()
		if err != nil {
			return nil, err
		}

		return &ConditionalExpr{Gate: gate, Target: target}, nil
	}

	return gate, nil
}

// parseFallback handles '||'
func (p *Parser) parseFallback() (Expr, error) {
	left, err := p.parsePipe()
	if err != nil {
		return nil, err
	}

	for p.tok.Type == TokenOr {
		p.next()

		right, err := p.parsePipe()
		if err != nil {
			return nil, err
		}

		left = &FallbackExpr{Left: left, Right: right}
	}

	return left, nil
}

// parsePipe handles '->' and '|'
func (p *Parser) parsePipe() (Expr, error) {
	left, err := p.parseParallel()
	if err != nil {
		return nil, err
	}

	for p.tok.Type == TokenArrow || p.tok.Type == TokenPipe {
		p.next()

		right, err := p.parseParallel()
		if err != nil {
			return nil, err
		}

		left = &PipelineExpr{Left: left, Right: right}
	}

	return left, nil
}

// parseParallel handles '&'
func (p *Parser) parseParallel() (Expr, error) {
	var children []Expr

	first, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	children = append(children, first)

	for p.tok.Type == TokenAmpersand {
		p.next()

		child, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}

		children = append(children, child)
	}

	if len(children) == 1 {
		return children[0], nil
	}

	return &ParallelExpr{Children: children}, nil
}

// parsePrimary handles atom, projection, loops, parentheses
func (p *Parser) parsePrimary() (Expr, error) {
	switch p.tok.Type {
	case TokenLParen:
		p.next() // consume '('

		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}

		if !p.expect(TokenRParen) {
			return nil, p.errorf("expected ')'")
		}

		return inner, nil

	case TokenLBrace:
		return p.parseProjection()

	case TokenIdent:
		if p.tok.Lit == "loop" {
			return p.parseLoop()
		}

		return p.parseAtom()

	default:
		return nil, p.errorf("unexpected token %q, expected identifier, '{', or '('", p.tok.Lit)
	}
}

// parseProjection safely reads raw code spanning out from inside braces
func (p *Parser) parseProjection() (Expr, error) {
	if p.tok.Type != TokenLBrace {
		return nil, p.errorf("expected '{'")
	}

	startPos := p.tok.Offset + 1
	depth := 1

	p.l.pos = startPos
	p.l.line = p.tok.Line
	p.l.col = p.tok.Col + 1

	for depth > 0 && p.l.pos < len(p.l.input) {
		ch := p.l.input[p.l.pos]
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endPos := p.l.pos
				p.l.pos++
				p.l.col++
				raw := strings.TrimSpace(p.l.input[startPos:endPos])
				p.tok = p.l.Next()
				p.peek = p.l.Next()

				return &ProjectionExpr{Raw: raw}, nil
			}
		}

		p.l.pos++

		p.l.col++
		if ch == '\n' {
			p.l.line++
			p.l.col = 1
		}
	}

	return nil, p.errorf("unclosed projection brace")
}

// parseLoop handles `loop( A ) until( B )`
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

	p.next() // consume 'until'

	if p.tok.Type != TokenLParen {
		return nil, p.errorf("expected '(' after until")
	}

	startPos := p.tok.Offset + 1
	depth := 1

	p.l.pos = startPos
	p.l.line = p.tok.Line
	p.l.col = p.tok.Col + 1

	for depth > 0 && p.l.pos < len(p.l.input) {
		ch := p.l.input[p.l.pos]
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				endPos := p.l.pos
				p.l.pos++
				p.l.col++
				raw := strings.TrimSpace(p.l.input[startPos:endPos])
				p.tok = p.l.Next()
				p.peek = p.l.Next()

				return &LoopExpr{Body: body, Until: raw}, nil
			}
		}

		p.l.pos++

		p.l.col++
		if ch == '\n' {
			p.l.line++
			p.l.col = 1
		}
	}

	return nil, p.errorf("unclosed until parenthesis")
}

// parseAtom: name [modifiers...] [(key=value, ...)]
func (p *Parser) parseAtom() (Expr, error) {
	if p.tok.Type != TokenIdent {
		return nil, p.errorf("expected identifier")
	}

	name := p.tok.Lit
	p.next()

	atom := &AtomExpr{
		Name:      name,
		Params:    make(map[string]string),
		Inputs:    make(map[string]string),
		Targets:   []string{},
		Excludes:  []string{},
		Modifiers: []string{},
	}

loop:
	for {
		switch p.tok.Type {
		case TokenColon:
			p.next()

			if p.tok.Type != TokenIdent {
				return nil, p.errorf("expected identifier after ':'")
			}

			modifier := p.tok.Lit
			p.next()

			if p.tok.Type == TokenAssign {
				modifier += "="

				p.next()

				for p.tok.Type == TokenIdent || p.tok.Type == TokenNumber || p.tok.Type == TokenString {
					modifier += p.tok.Lit
					p.next()
				}
			}

			atom.Modifiers = append(atom.Modifiers, modifier)
			if atom.Profile == "" && !strings.Contains(modifier, "=") {
				atom.Profile = modifier
			}
		case TokenAt:
			p.next()

			if p.tok.Type != TokenIdent && p.tok.Type != TokenString {
				return nil, p.errorf("expected prompt string after '@'")
			}

			atom.Prompt = p.tok.Lit
			p.next()
		case TokenHash:
			p.next()

			if p.tok.Type != TokenIdent {
				return nil, p.errorf("expected target after '#'")
			}

			atom.Targets = append(atom.Targets, p.tok.Lit)
			p.next()

			for p.tok.Type == TokenComma {
				p.next()

				if p.tok.Type != TokenIdent {
					break
				}

				atom.Targets = append(atom.Targets, p.tok.Lit)
				p.next()
			}
		case TokenTilde:
			p.next()

			if p.tok.Type != TokenIdent {
				return nil, p.errorf("expected exclude after '~'")
			}

			atom.Excludes = append(atom.Excludes, p.tok.Lit)
			p.next()

			for p.tok.Type == TokenComma {
				p.next()

				if p.tok.Type != TokenIdent {
					break
				}

				atom.Excludes = append(atom.Excludes, p.tok.Lit)
				p.next()
			}
		case TokenLParen:
			p.next()

			for p.tok.Type != TokenRParen && p.tok.Type != TokenEOF {
				if p.tok.Type != TokenIdent {
					return nil, p.errorf("expected identifier in param")
				}

				key := p.tok.Lit
				p.next()

				if !p.expect(TokenAssign) {
					return nil, p.errorf("expected '=' after key")
				}

				if p.tok.Type != TokenIdent && p.tok.Type != TokenString && p.tok.Type != TokenNumber {
					return nil, p.errorf("expected value")
				}

				val := p.tok.Lit
				p.next()

				if strings.Contains(val, ".") {
					atom.Inputs[key] = val
				} else {
					atom.Params[key] = val
				}

				if p.tok.Type == TokenComma {
					p.next()
				}
			}

			if !p.expect(TokenRParen) {
				return nil, p.errorf("expected ')'")
			}
		default:
			break loop
		}
	}

	return atom, nil
}
