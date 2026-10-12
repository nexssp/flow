package match

import (
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

type matchKeyword struct{}

func (matchKeyword) Name() string    { return "match" }
func (matchKeyword) Keyword() string { return "match" }

func (matchKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	p.Advance()

	subject := ""
	if p.Current().Type == compiler.TokLParen {
		p.Advance()
		subjStart := p.Current().Offset
		depth := 1

		for p.Current().Type != compiler.TokEOF {
			if p.Current().Type == compiler.TokLParen {
				depth++
			} else if p.Current().Type == compiler.TokRParen {
				depth--
				if depth == 0 {
					break
				}
			}
			p.Advance()
		}

		if p.Current().Type != compiler.TokRParen {
			return nil, p.Fail(p.Current().Line, "unclosed '(' in match subject")
		}

		subject = strings.TrimSpace(p.Src()[subjStart:p.Current().Offset])
		p.Advance()
	}

	if p.Current().Type != compiler.TokLBrace {
		return nil, p.Fail(p.Current().Line, "expected '{' after match")
	}
	p.Advance()

	var cases []Case

	for p.Current().Type != compiler.TokRBrace && p.Current().Type != compiler.TokEOF {
		var cond string
		isDefault := false

		if p.Current().Type == compiler.TokIdent && (p.Current().Lit == "_" || p.Current().Lit == "default") {
			isDefault = true
			p.Advance()
		} else {
			condStart := p.Current().Offset
			for p.Current().Type != compiler.TokEOF && p.Current().Type != compiler.TokArrow {
				p.Advance()
			}
			cond = strings.TrimSpace(p.Src()[condStart:p.Current().Offset])
			if cond == "" {
				return nil, p.Fail(p.Current().Line, "expected condition expression before '->'")
			}
		}

		if p.Current().Type != compiler.TokArrow {
			return nil, p.Fail(p.Current().Line, "expected '->' after match condition")
		}
		p.Advance()

		armExpr, err := p.ParseExpr(0)
		if err != nil {
			return nil, err
		}

		cases = append(cases, Case{
			Condition: cond,
			IsDefault: isDefault,
			Body:      armExpr,
		})

		if p.Current().Type == compiler.TokComma {
			p.Advance()
		}
	}

	if p.Current().Type != compiler.TokRBrace {
		return nil, p.Fail(p.Current().Line, "expected '}' closing match block")
	}
	p.Advance()

	return &Expr{
		Subject: subject,
		Cases:   cases,
	}, nil
}
