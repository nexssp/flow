package assert

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// Directive parses `@assert: EXPR` and appends EXPR to meta["asserts"].
// The colon after "assert" is optional: `@assert: x == 1` and
// `@assert x == 1` are equivalent.
var Directive = compiler.Directive{
	Name:     "assert",
	Example:  `@assert: result != nil`,
	Handler:  handleDirective,
	Metadata: compiler.AppendMetadata[string]("asserts"),
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	expression := strings.TrimSpace(strings.TrimPrefix(line, "@assert"))
	expression = strings.TrimPrefix(expression, ":")
	expression = strings.TrimSpace(compiler.StripExprComments(expression))

	if expression == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@assert: expression is required",
		)
	}

	existing, _ := req.Out["asserts"].([]string)
	req.Out["asserts"] = append(existing, expression)
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}
