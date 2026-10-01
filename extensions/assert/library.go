// Package assert provides the `assert(cond, msg)` keyword and the
// `@assert: expr` directive. The keyword yields a core.AssertExpr
// (evaluated later by the runtime with expr-lang); the directive
// appends its expression to meta["asserts"]. Core does not know what
// an assertion is: the parser dispatches the keyword through the
// bundle's Primaries, and the directive is handled by the bundle's
// Directive table.
//
// Typical use:
//
//	@assert: result.status == "ok"
//	{ score: 95 } -> assert(.score >= 50, "score too low")
package assert

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "assert"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{Directive},
		Primaries:  []core.PrimaryExtension{assertKeyword{}},
		SelfTest:   selftest,
		Fixtures:   fixturesFS,
	}
}
