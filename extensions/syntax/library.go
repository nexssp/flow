// Package syntax ships the four binary operators every .nflow file
// relies on: ->, |, &, ||, plus the structured parallel and race forms.
//
// The package is a native bundle — always mounted by native.Bundles()
// and never requires @require. It lives under extensions/ rather than
// compiler/ so the compiler remains a pure mechanism: the parser knows how to call
// an operator, not what a pipe means.
package syntax

import (
	"context"
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "syntax"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Operators: []compiler.Operator{pipe, pipeAlias, parallel, fallback},
		Primaries: []compiler.PrimaryExtension{namedParallelKeyword{}, raceKeyword{}},
		Fixtures:  fixturesFS,
	}
}

var pipe = compiler.Operator{
	Meta: compiler.OperatorMeta{
		Name:          "pipe",
		Token:         compiler.TokArrow,
		Precedence:    7,
		Associativity: compiler.Left,
		Description:   "Pass left output to right input",
		Example:       `runtime.const(value=hi) -> runtime.debug`,
	},
	Handler: pipeHandler,
}

var pipeAlias = compiler.Operator{
	Meta: compiler.OperatorMeta{
		Name:          "pipe_alias",
		Token:         compiler.TokPipe,
		Precedence:    7,
		Associativity: compiler.Left,
		Description:   "Syntax sugar for ->; uses the same pipeline semantics",
		Example:       `runtime.const(value=hi) | runtime.debug`,
	},
	Handler: pipeHandler,
}

func pipeHandler(_ context.Context, req compiler.OperatorReq) (compiler.OperatorRes, error) {
	return compiler.OperatorRes{Node: &compiler.PipeExpr{L: req.Left, R: req.Right}}, nil
}

var parallel = compiler.Operator{
	Meta: compiler.OperatorMeta{
		Name:          parallelKeyword,
		Token:         compiler.TokAmpersand,
		Precedence:    5,
		Associativity: compiler.Left,
		Description:   "Run branches concurrently",
		Example:       `( a & b & c )`,
	},
	Handler: func(_ context.Context, req compiler.OperatorReq) (compiler.OperatorRes, error) {
		return compiler.OperatorRes{
			Node: &compiler.ParallelExpr{Branches: []compiler.Expr{req.Left, req.Right}},
		}, nil
	},
}

var fallback = compiler.Operator{
	Meta: compiler.OperatorMeta{
		Name:          "fallback",
		Token:         compiler.TokOrOr,
		Precedence:    3,
		Associativity: compiler.Right,
		Description:   "Use right branch after left error",
		Example:       `a || b`,
	},
	Handler: func(_ context.Context, req compiler.OperatorReq) (compiler.OperatorRes, error) {
		return compiler.OperatorRes{
			Node: &compiler.FallbackExpr{L: req.Left, R: req.Right},
		}, nil
	},
}
