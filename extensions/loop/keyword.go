package loop

import (
	"strings"

	"github.com/nexssp/flow/core"
)

// loopKeyword is the compile-time parser extension. It has no runtime
// state; every expansion produces a fresh core.LoopExpr.
type loopKeyword struct{}

func (loopKeyword) Name() string    { return "loop" }
func (loopKeyword) Keyword() string { return "loop" }

func (loopKeyword) Parse(p *core.Parser) (core.Expr, error) {
	p.Advance() // consume 'loop'
	if p.Current().Type != core.TokLParen {
		return nil, p.Fail(p.Current().Line, "expected '(' after loop")
	}
	p.Advance()

	body, err := p.ParseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.Current().Type != core.TokRParen {
		return nil, p.Fail(p.Current().Line, "expected ')' after loop body")
	}
	p.Advance()

	if p.Current().Type != core.TokIdent || p.Current().Lit != "until" {
		return nil, p.Fail(p.Current().Line, "expected 'until' after loop body")
	}
	p.Advance()

	if p.Current().Type != core.TokLParen {
		return nil, p.Fail(p.Current().Line, "expected '(' after until")
	}
	p.Advance()

	// Scan the raw text between the outer parens so the condition
	// survives as written — commas, dots, and nested parens included.
	// It is compiled later by the runtime with expr-lang, not here.
	untilStart := p.Current().Offset
	untilDepth := 1

scan:
	for p.Current().Type != core.TokEOF {
		switch p.Current().Type {
		case core.TokLParen:
			untilDepth++
		case core.TokRParen:
			untilDepth--
			if untilDepth == 0 {
				break scan
			}
		case core.TokEOF, core.TokIdent, core.TokString, core.TokNumber,
			core.TokArrow, core.TokPipe, core.TokAmpersand, core.TokOrOr,
			core.TokLBrace, core.TokRBrace, core.TokColon, core.TokEquals,
			core.TokComma, core.TokQuestion, core.TokHash, core.TokTilde,
			core.TokAtBrace, core.TokAtPrompt:
		}
		p.Advance()
	}

	if p.Current().Type != core.TokRParen {
		return nil, p.Fail(p.Current().Line, "unclosed 'until(...)' condition")
	}

	rawCondition := strings.TrimSpace(p.Src()[untilStart:p.Current().Offset])
	p.Advance() // consume closing ')'

	return &core.LoopExpr{Body: body, Until: rawCondition}, nil
}
