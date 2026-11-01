// Package include provides the `@include "file.nflow" [AS namespace]` source-module import.
// The compiler preprocesses each imported file in its own declaration scope and
// resolver. Its exported capabilities and symbols are qualified by the explicit
// alias or, by default, the filename stem; imported @func names are available
// below that same alias, including composed nested aliases. Child declarations
// are never merged into the parent's scope. `@require` remains independent.
//
// For example, `@include ./shared/payments.nflow AS billing` exposes a child
// pipeline as `billing.pipeline.charge`.
package include

import (
	"context"
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "include"

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
		Fixtures:   fixturesFS,
	}
}

var _ = context.Background
