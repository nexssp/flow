package assert

import (
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// assertKeyword parses `assert(cond)` or `assert(cond, "msg")` and
// returns a compiler.AssertExpr. The condition is preserved as raw text and
// compiled later by the runtime with expr-lang.
type assertKeyword struct{}

func (assertKeyword) Name() string    { return "assert" }
func (assertKeyword) Keyword() string { return "assert" }

func (assertKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	startPos := p.Position()
	p.Advance() // consume 'assert'
	if p.Current().Type != compiler.TokLParen {
		return nil, p.Fail(p.Current().Line, "expected '(' after assert")
	}
	p.Advance()

	// Scan for the matching close paren, tracking nested parens so a
	// condition like contains([1,2], x) survives intact.
	conditionStart := p.Current().Offset
	depth := 1

scan:
	for p.Current().Type != compiler.TokEOF {
		switch p.Current().Type {
		case compiler.TokLParen:
			depth++
		case compiler.TokRParen:
			depth--
			if depth == 0 {
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
		return nil, p.Fail(p.Current().Line, "unclosed 'assert(...)' condition")
	}

	raw := strings.TrimSpace(p.Src()[conditionStart:p.Current().Offset])
	p.Advance() // consume closing ')'

	condition, message := splitConditionMessage(raw)
	return &compiler.AssertExpr{
		Condition: condition,
		Message:   message,
		Pos:       startPos,
	}, nil
}

// splitConditionMessage splits at the first top-level comma so commas
// inside function calls, arrays, or quoted strings are preserved:
//
//	assert(contains([1,2], x), "msg")  →  "contains([1,2], x)", "msg"
//	assert(.score >= 50)               →  ".score >= 50", ""
func splitConditionMessage(raw string) (condition, message string) {
	commaIndex := compiler.IndexTopLevel(raw, ',')
	if commaIndex < 0 {
		return strings.TrimSpace(raw), ""
	}
	condition = strings.TrimSpace(raw[:commaIndex])
	message = strings.Trim(strings.TrimSpace(raw[commaIndex+1:]), `"'`+"`")
	return condition, message
}
