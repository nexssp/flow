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

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "include"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{Directive},
		SelfTest:   selftest,
	}
}

var _ = context.Background
