package syntax

import (
	"github.com/nexssp/flow/compiler"
)

const bindingKeyword = "as"

type namedBindingPostfix struct{}

func (namedBindingPostfix) Name() string                     { return "named_binding" }
func (namedBindingPostfix) PostfixToken() compiler.TokenType { return compiler.TokIdent }
func (namedBindingPostfix) MatchesPostfix(p *compiler.Parser) bool {
	return p.Current().Type == compiler.TokIdent && p.Current().Lit == bindingKeyword
}

func (namedBindingPostfix) Parse(p *compiler.Parser) (compiler.Expr, error) {
	return nil, p.Fail(p.Current().Line, "`as name` is a postfix binding, not a primary")
}

func (namedBindingPostfix) ParsePostfix(p *compiler.Parser, left compiler.Expr) (compiler.Expr, error) {
	position := p.Position()
	p.Advance() // consume `as`
	if p.Current().Type != compiler.TokIdent {
		return nil, p.Fail(p.Current().Line, "expected a binding name after `as`")
	}
	if _, alreadyBound := left.(*compiler.BindingExpr); alreadyBound {
		return nil, p.Fail(position.Line, "a pipeline step may have only one `as` binding")
	}
	name := p.Current().Lit
	if err := compiler.ValidateExpressionBindingName(name); err != nil {
		return nil, p.Fail(p.Current().Line, "%v", err)
	}
	p.Advance()
	return &compiler.BindingExpr{Inner: left, Name: name, Pos: position}, nil
}
