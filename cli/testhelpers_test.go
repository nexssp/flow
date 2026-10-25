package cli

import "github.com/nexssp/flow/compiler"

type lspTestPrimary struct{}

func (lspTestPrimary) Name() string    { return "lsp-test-primary" }
func (lspTestPrimary) Keyword() string { return "lsp_test" }
func (lspTestPrimary) Parse(p *compiler.Parser) (compiler.Expr, error) {
	p.Advance()
	return p.ParseExpr(0)
}
