package loop

import (
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// loopKeyword is the compile-time parser extension. It has no runtime
// state; every expansion produces a fresh compiler.LoopExpr.
type loopKeyword struct{}

func (loopKeyword) Name() string    { return "loop" }
func (loopKeyword) Keyword() string { return "loop" }

func (loopKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	startPos := p.Position()
	p.Advance() // consume 'loop'
	if p.Current().Type != compiler.TokLParen {
		return nil, p.Fail(p.Current().Line, "expected '(' after loop")
	}
	p.Advance()

	body, err := p.ParseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.Current().Type != compiler.TokRParen {
		return nil, p.Fail(p.Current().Line, "expected ')' after loop body")
	}
	p.Advance()

	if p.Current().Type != compiler.TokIdent || p.Current().Lit != "until" {
		return nil, p.Fail(p.Current().Line, "expected 'until' after loop body")
	}
	p.Advance()

	if p.Current().Type != compiler.TokLParen {
		return nil, p.Fail(p.Current().Line, "expected '(' after until")
	}
	p.Advance()

	// Scan the raw text between the outer parens so the condition
	// survives as written — commas, dots, and nested parens included.
	// It is compiled later by the runtime with expr-lang, not here.
	untilStart := p.Current().Offset
	untilDepth := 1

scan:
	for p.Current().Type != compiler.TokEOF {
		switch p.Current().Type {
		case compiler.TokLParen:
			untilDepth++
		case compiler.TokRParen:
			untilDepth--
			if untilDepth == 0 {
				break scan
			}
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
			compiler.TokLBrace, compiler.TokRBrace, compiler.TokColon, compiler.TokEquals,
			compiler.TokComma, compiler.TokQuestion, compiler.TokHash, compiler.TokTilde,
			compiler.TokAtBrace, compiler.TokAtPrompt:
		}
		p.Advance()
	}

	if p.Current().Type != compiler.TokRParen {
		return nil, p.Fail(p.Current().Line, "unclosed 'until(...)' condition")
	}

	rawCondition := strings.TrimSpace(p.Src()[untilStart:p.Current().Offset])
	p.Advance() // consume closing ')'

	return &compiler.LoopExpr{Body: body, Until: rawCondition, Pos: startPos}, nil
}
