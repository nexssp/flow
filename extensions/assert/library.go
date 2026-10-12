// Package assert provides the `assert(cond, msg)` keyword and the
// `@assert: expr` directive. The keyword yields a compiler.AssertExpr
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

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "assert"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []compiler.Directive{Directive},
		Primaries:  []compiler.PrimaryExtension{assertKeyword{}},
		Fixtures:   fixturesFS,
	}
}
