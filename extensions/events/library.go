// Package events provides the generic, typed @events route directive.
package events

import (
	"embed"

	"github.com/nexssp/flow/compiler"
)

const ID = "events"

//go:embed nflows
var fixtures embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:               ID,
		Directives:       []compiler.Directive{Directive},
		Fixtures:         fixtures,
		Materialize:      MaterializeRoutes,
		MaterializePhase: compiler.MaterializeCompositions,
	}
}
