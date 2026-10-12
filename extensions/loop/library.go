// Package loop provides the `loop(body) until(cond)` keyword. It is a
// KeywordPrimary: the parser dispatches on the "loop" identifier when
// this bundle is registered.
//
// Typical use:
//
//	{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
//
// DSL fixtures live under nflows/ and are discovered by
// compiler.Bundle.AllSelfTests; there is no inline SelfTest.
package loop

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "loop"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Primaries: []compiler.PrimaryExtension{loopKeyword{}},
		Fixtures:  fixturesFS,
	}
}
