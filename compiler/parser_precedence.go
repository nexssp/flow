package compiler

// The precedence chain, loosest to tightest:
//
//     conditional   a ? b : c
//     fallback      a || b
//     pipe          a -> b   |   a | b
//     parallel      a & b
//     primary       (…), {…}, atom, loop, assert
//
// Each function consumes tokens at its own level and delegates the next
// tighter level to the function below. parseConditional is the entry point
// (see parser.go:parseExpr).

// parseConditional handles `gate ? target` and `gate ? target : else`.
//
// The ':' that introduces the else arm is distinguished from a modifier
// colon by whitespace: a modifier colon is glued to its key
// (`atom:timeout=30s`), a ternary colon is not (`a ? b : c`). The
// whitespace test lives in isTernaryColon (parser_atom.go).
func (p *Parser) parseConditional() (Expr, error) {
	gate, err := p.parseFallback()
	if err != nil {
		return nil, err
	}

	if p.tok.Type != TokenQuestion {
		return gate, nil
	}
	p.next()

	target, err := p.parseFallback()
	if err != nil {
		return nil, err
	}

	if p.tok.Type != TokenColon || !p.isTernaryColon() {
		return &ConditionalExpr{Gate: gate, Target: target}, nil
	}
	p.next()

	// Right-associative: `a ? b : c ? d : e` parses as `a ? b : (c ? d : e)`.
	elseExpr, err := p.parseConditional()
	if err != nil {
		return nil, err
	}

	return &ConditionalExpr{Gate: gate, Target: target, Else: elseExpr}, nil
}

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

func (p *Parser) parseParallel() (Expr, error) {
	first, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	children := []Expr{first}

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

func (p *Parser) parsePrimary() (Expr, error) {
	switch p.tok.Type {
	case TokenLParen:
		p.next()
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
		switch p.tok.Lit {
		case "loop":
			return p.parseLoop()
		case "assert":
			return p.parseAssert()
		}
		return p.parseAtom()

	default:
		return nil, p.errorf(
			"unexpected token %q, expected identifier, '{', or '('", p.tok.Lit)
	}
}
