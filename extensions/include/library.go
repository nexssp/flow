// Package include provides the `@include "file.nflow"` directive. The
// referenced file is preprocessed recursively in the caller's context
// (cycle detection uses the include chain), and its pipelines,
// declarations, and require block are merged into the parent's meta.
//
// Typical use:
//
//	@include ./shared/child.nflow
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
