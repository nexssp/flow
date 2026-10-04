// Package syntax ships the four binary operators every .nflow file
// relies on: ->, |, &, ||. Without them, no pipeline is expressible.
//
// The package is a native bundle — always mounted by native.Bundles()
// and never requires @require. It lives under extensions/ rather than
// core/ so core remains a pure mechanism: the parser knows how to call
// an operator, not what a pipe means.
package syntax

import (
	"context"
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "syntax"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Operators: []core.Operator{pipe, pipeAlias, parallel, fallback},
		Primaries: []core.PrimaryExtension{namedParallelKeyword{}},
		SelfTest:  selftest,
		Fixtures:  fixturesFS,
	}
}

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Operators",
			Features: []core.SelfTestFeature{
				{
					Name: "Sequential pipe ->",
					DSL: `@assert: result == "piped"
runtime.const @{ value: "piped" } -> runtime.noop`,
				},
				{
					Name: "Pipe syntax sugar |",
					DSL: `@assert: result == "same pipeline result"
runtime.const @{ value: "same pipeline result" } | runtime.noop`,
				},
				{
					Name: "Parallel &",
					DSL: `@assert: result != nil
( runtime.const @{ value: "left" } & runtime.const @{ value: "right" } )`,
				},
				{
					Name: "Fallback ||",
					DSL: `@assert: result == "recovered"
runtime.fail @{ message: "boom", kind: "Unavailable" } || runtime.const @{ value: "recovered" }`,
				},
			},
		},
	}
}

var pipe = core.Operator{
	Meta: core.OperatorMeta{
		Name:          "pipe",
		Token:         core.TokArrow,
		Precedence:    7,
		Associativity: core.Left,
		Description:   "Pass left output to right input",
		Example:       `runtime.const(value=hi) -> runtime.debug`,
	},
	Handler: pipeHandler,
}

var pipeAlias = core.Operator{
	Meta: core.OperatorMeta{
		Name:          "pipe_alias",
		Token:         core.TokPipe,
		Precedence:    7,
		Associativity: core.Left,
		Description:   "Syntax sugar for ->; uses the same pipeline semantics",
		Example:       `runtime.const(value=hi) | runtime.debug`,
	},
	Handler: pipeHandler,
}

func pipeHandler(_ context.Context, req core.OperatorReq) (core.OperatorRes, error) {
	return core.OperatorRes{Node: &core.PipeExpr{L: req.Left, R: req.Right}}, nil
}

var parallel = core.Operator{
	Meta: core.OperatorMeta{
		Name:          "parallel",
		Token:         core.TokAmpersand,
		Precedence:    5,
		Associativity: core.Left,
		Description:   "Run branches concurrently",
		Example:       `( a & b & c )`,
	},
	Handler: func(_ context.Context, req core.OperatorReq) (core.OperatorRes, error) {
		return core.OperatorRes{
			Node: &core.ParallelExpr{Branches: []core.Expr{req.Left, req.Right}},
		}, nil
	},
}

var fallback = core.Operator{
	Meta: core.OperatorMeta{
		Name:          "fallback",
		Token:         core.TokOrOr,
		Precedence:    3,
		Associativity: core.Right,
		Description:   "Use right branch after left error",
		Example:       `a || b`,
	},
	Handler: func(_ context.Context, req core.OperatorReq) (core.OperatorRes, error) {
		return core.OperatorRes{
			Node: &core.FallbackExpr{L: req.Left, R: req.Right},
		}, nil
	},
}
