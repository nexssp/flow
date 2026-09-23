package compiler

// Parser walks the token stream produced by Lexer and builds an AST.
//
// The parser is split across four files:
//
//	parser.go              — Parser struct, token advance, entry points
//	parser_precedence.go   — the operator precedence chain
//	parser_atom.go         — atoms, modifiers, ternary-colon detection
//	parser_blocks.go       — brace / paren / bracket scanning, projections,
//	                         loops, asserts
//
// The split mirrors the grammar: each file owns one level of the language.
type Parser struct {
	l    *Lexer
	file string
	tok  Token
	peek Token
	err  error
}

func NewParser(input string) *Parser {
	return NewParserWithFile(input, "")
}

func NewParserWithFile(input, file string) *Parser {
	p := &Parser{l: NewLexer(input), file: file}
	p.next()
	p.next()
	return p
}

// next advances the token stream by one. When a lexer error is already
// recorded, next is a no-op: the first error wins, and every subsequent
// call would just overwrite it with noise.
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

// expect consumes a token of the given type, or records a parse error.
func (p *Parser) expect(tt TokenType) bool {
	if p.tok.Type == tt {
		p.next()
		return true
	}
	p.err = p.errorf("expected %s, got %s", tt, p.tok.Type)
	return false
}

func (p *Parser) errorf(format string, args ...any) error {
	return SourceError(Position{
		File: p.file,
		Line: p.tok.Line,
		Col:  p.tok.Col,
	}, format, args...)
}

// resync re-seeds the two-token lookahead after a raw scan advanced the
// lexer position without going through next(). It clears any error the
// scan may have left behind, because the scan is the authority on what
// was consumed.
func (p *Parser) resync() {
	p.err = nil
	p.tok = p.l.Next()
	p.peek = p.l.Next()
	if p.peek.Type == TokenInvalid {
		p.err = p.errorf("invalid character: %q", p.peek.Lit)
	}
}

// ParseExpression parses a complete pipeline expression and requires that
// the token stream is exhausted when it returns.
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

// parseExpr is the entry point to the precedence chain. The chain, from
// loosest to tightest binding, is defined in parser_precedence.go.
func (p *Parser) parseExpr() (Expr, error) {
	return p.parseConditional()
}
