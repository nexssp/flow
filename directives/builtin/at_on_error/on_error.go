package at_on_error

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
)

const declarationKey = "on_error"

// Decl is the parsed `@on_error { ... }` block.
type Decl struct {
	Pos   core.Position
	Rules []core.RecoveryRule
	Else  string
}

type directive struct{}

func init() {
	d := directive{}
	core.Register(d)
	core.RegisterPipelineWrapper(d)
}

func (directive) Name() string { return "on_error" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "on_error", err.Error())
	}

	rest, ok := core.StripDirectivePrefix(header, "on_error")
	if !ok {
		return 0, core.AtErr(ctx, i, "on_error", "malformed directive")
	}
	if strings.TrimSpace(rest) != "" {
		return 0, core.AtErr(ctx, i, "on_error",
			"takes no name; expected bare `@on_error { ... }`")
	}

	if _, exists := ctx.Out.Declarations[declarationKey]; exists {
		return 0, core.AtErr(ctx, i, "on_error",
			"duplicate; at most one per file in v1")
	}

	rules, els, err := core.ParseRecoveryBlock(body, ctx, i, "on_error")
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 && els == "" {
		return 0, core.AtErr(ctx, i, "on_error",
			"needs at least one `when ...` clause or an `else`")
	}

	ctx.Out.Declarations[declarationKey] = Decl{
		Pos:   core.Position{File: ctx.File, Line: i + 1},
		Rules: rules,
		Else:  els,
	}
	return next, nil
}

// WrapPipeline attaches a recovery node to the compiled pipeline when
// the file declared an @on_error. Returns inner unchanged when the
// declaration is absent.
func (directive) WrapPipeline(pre *core.Preprocessed, inner action.Executable) (action.Executable, error) {
	raw, ok := pre.Declarations[declarationKey]
	if !ok {
		return inner, nil
	}
	decl, ok := raw.(Decl)
	if !ok {
		return nil, fmt.Errorf(
			"@on_error: declaration type %T not Decl (internal invariant broken)", raw)
	}

	return core.NewRecoveryAction("on_error", inner, core.RecoverySpec{
		Rules: decl.Rules,
		Else:  decl.Else,
	})
}
