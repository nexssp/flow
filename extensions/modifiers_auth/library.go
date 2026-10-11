// Package modifiers_auth ships the identity guards :auth, :role=,
// :perm=, and :feature=. Each compiles into a middleware that checks
// the request context; without one of them the action runs ungated.
//
// Typical use:
//
//	payments.charge:auth:role=admin:perm=payments:write
package modifiers_auth

import (
	"embed"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

const ID = "modifiers_auth"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Modifiers: Modifiers(),
		Fixtures:  fixturesFS,
	}
}
