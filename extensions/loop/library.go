// Package loop provides the `loop(body) until(cond)` keyword. It is a
// KeywordPrimary: the parser dispatches on the "loop" identifier when
// this bundle is registered.
//
// Typical use:
//
//	{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
//
// DSL fixtures live under nflows/ and are discovered by
// core.Bundle.AllSelfTests; there is no inline SelfTest.
package loop

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "loop"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Primaries: []core.PrimaryExtension{loopKeyword{}},
		SelfTest:  selftest,
		Fixtures:  fixturesFS,
	}
}
