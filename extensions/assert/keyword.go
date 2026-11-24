package assert

import (
	"strings"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/internal/exprsyntax"
)

// assertKeyword parses `assert(cond)` and optional literal or expression messages.
// Expressions are preserved as raw text and compiled later by the runtime.
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
		case compiler.TokInvalid:
			if !exprsyntax.IsExpressionPunctuation(p.Current().Lit) {
				return nil, p.Fail(p.Current().Line, "unexpected character %q", p.Current().Lit)
			}
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
			compiler.TokLBrace, compiler.TokRBrace, compiler.TokLBracket, compiler.TokRBracket,
			compiler.TokColon, compiler.TokEquals,
			compiler.TokComma, compiler.TokQuestion, compiler.TokHash, compiler.TokTilde,
			compiler.TokAtBrace, compiler.TokAtPrompt:
			// The condition scanner tracks parentheses; other tokens stay in the raw condition.
		}
		p.Advance()
	}

	if p.Current().Type != compiler.TokRParen {
		return nil, p.Fail(p.Current().Line, "unclosed 'assert(...)' condition")
	}

	raw := strings.TrimSpace(p.Src()[conditionStart:p.Current().Offset])
	p.Advance() // consume closing ')'

	condition, message, messageExpression := splitConditionMessage(raw)
	return &compiler.AssertExpr{
		Condition:         condition,
		Message:           message,
		MessageExpression: messageExpression,
		Pos:               startPos,
	}, nil
}

// splitConditionMessage splits at the first top-level comma so commas
// inside function calls, arrays, or quoted strings are preserved. A wholly
// quoted second argument remains a static message; other arguments are expressions:
//
//	assert(contains([1,2], x), "msg")  →  "contains([1,2], x)", "msg", ""
//	assert(.score >= 50)               →  ".score >= 50", "", ""
func splitConditionMessage(raw string) (condition, message, messageExpression string) {
	commaIndex := compiler.IndexTopLevel(raw, ',')
	if commaIndex < 0 {
		return strings.TrimSpace(raw), "", ""
	}
	condition = strings.TrimSpace(raw[:commaIndex])
	messageSource := strings.TrimSpace(raw[commaIndex+1:])
	if message, ok := quotedMessage(messageSource); ok {
		return condition, message, ""
	}
	return condition, "", messageSource
}

func quotedMessage(source string) (string, bool) {
	if len(source) < 2 || (source[0] != '"' && source[0] != '\'' && source[0] != '`') {
		return "", false
	}
	quote := source[0]
	for i := 1; i < len(source); i++ {
		if quote != '`' && source[i] == '\\' && i+1 < len(source) {
			i++
			continue
		}
		if source[i] == quote {
			if i != len(source)-1 {
				return "", false
			}
			return source[1:i], true
		}
	}
	return "", false
}
