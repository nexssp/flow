package directives

import "strings"

type atAssert struct{}

func init() { Register(atAssert{}) }

func (atAssert) Name() string { return "assert" }

// Syntax:
//
//	@assert: success == true
//	@assert: cost_usd < 0.50
//
// Every @assert: directive is collected into Preprocessed.Asserts and
// evaluated after the flow finishes. The expression language is the
// one supplied by the runner (github.com/expr-lang/expr in the
// current implementation); the directive does not validate the
// expression itself, only its presence.
//
// The directive preserves the original line in Body so that
// runner/assertions.go's text-scanning mergeAssertions keeps working
// until it is migrated to read Preprocessed.Asserts directly.
func (atAssert) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	expr, ok := StripDirectivePrefix(line, "assert")
	if !ok {
		return 0, AtErr(ctx, i, "assert", "malformed directive")
	}
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return 0, AtErr(ctx, i, "assert", "requires an expression")
	}

	ctx.Out.Asserts = append(ctx.Out.Asserts, AssertDecl{
		Expr: expr,
		Pos:  Position{File: ctx.File, Line: i + 1},
	})

	// Do NOT store raw directive lines in ctx.Body.
	// Directives are compiled into Preprocessed, not passed down to the pipeline parser.
	return i + 1, nil
}
