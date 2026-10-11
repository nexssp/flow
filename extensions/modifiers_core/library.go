// Package modifiers_core ships the execution modifiers :timeout=,
// :retry=, :concurrency=, :cache=, :coalesce, :dedup, :idempotent,
// and :rate_limit=. Each calls a Builder method that installs
// middleware; none is metadata-only.
//
// Typical use:
//
//	payment.charge:timeout=5s:retry=3:idempotent:rate_limit=100
package modifiers_core

import (
	"embed"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "modifiers_core"

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
