// Package on_error provides the `@on_error { when ... -> target; else
// -> target }` block. On a runtime error, WrapPipeline wraps the
// compiled program with a CatchAny handler that evaluates each rule's
// condition against the error and routes to the matching target.
//
// Typical use:
//
//	@on_error {
//	  when error.kind == "Timeout" -> cov.recoverable
//	  when error.kind == "NotFound" -> log.info
//	  else -> error.info
//	}
package on_error

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "on_error"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:           ID,
		Libraries:    []action.Library{Library()},
		Directives:   []core.Directive{Directive},
		Primaries:    []core.PrimaryExtension{errorGuardPrimary{}},
		WrapPipeline: wrapFromMeta,
		Fixtures:     fixturesFS,
	}
}

// Library returns the extension's directives and the error.info action
// as a kernel library.
func Library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			Directive.Action(),
			ErrorInfoAction,
		},
	}
}
