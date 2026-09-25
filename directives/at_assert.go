package directives

import "strings"

type atAssert struct{}

func init() { Register(atAssert{}) }

func (atAssert) Name() string { return "assert" }

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

	return i + 1, nil
}
