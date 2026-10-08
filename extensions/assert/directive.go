package assert

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// Directive parses `@assert: EXPR` and appends EXPR to meta["asserts"].
// The colon after "assert" is optional: `@assert: x == 1` and
// `@assert x == 1` are equivalent.
var Directive = core.Directive{
	Name:    "assert",
	Example: `@assert: result != nil`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	expression := strings.TrimSpace(strings.TrimPrefix(line, "@assert"))
	expression = strings.TrimPrefix(expression, ":")
	expression = strings.TrimSpace(core.StripExprComments(expression))

	if expression == "" {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@assert: expression is required",
		)
	}

	existing, _ := req.Out["asserts"].([]string)
	req.Out["asserts"] = append(existing, expression)
	return core.DirectiveRes{Next: req.I + 1}, nil
}
