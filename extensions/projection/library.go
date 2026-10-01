// Package projection adds the `{ ... }` projection syntax. The parser
// dispatches the opening `{` through this bundle's PrimaryExtension;
// the ProjectionExpr it produces is lowered at build time to the
// projection runtime action, which evaluates the body with expr-lang.
// Without this bundle, `{ }` in expression position is a syntax error.
//
// Typical use:
//
//	{ name: "Ada", extra: true } -> { name: .name }
//	{ id: 1, status: "new" } -> { ..., status: "active" }
package projection

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "projection"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library(NewExprEvaluator())},
		Primaries: []core.PrimaryExtension{Primary()},
		Fixtures:  fixturesFS,
	}
}

// Primary returns the parser extension. Exposed so tests can build a
// parser without spinning up the whole bundle.
func Primary() core.PrimaryExtension { return Extension{} }

// Library returns the runtime projection action bound to eval. A nil
// eval falls back to DefaultEvaluator, which is a pass-through — used
// only in isolated parser tests.
func Library(eval Evaluator) action.Library {
	if eval == nil {
		eval = DefaultEvaluator
	}
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			projectionAction(eval),
		},
	}
}
