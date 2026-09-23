package flow

import (
	"github.com/nexssp/flow/nodes/utility"
	"github.com/nexssp/kernel/action"
)

// BaseLibrary returns a small set of always-useful actions that any
// .nflow file can rely on without project-specific Go registration.
//
// It is deliberately separate from StandardLibrary:
//
//   - StandardLibrary is orchestration mechanics (log, bench,
//     distribute, dispatch, supervisor).
//   - BaseLibrary is small data and environment primitives that make
//     a pipeline self-contained — stubs, shape transforms, config from
//     env, generated IDs, dynamic dispatch, and a way to force an
//     error.
//
// Applications should mount both:
//
//	libs := []action.Library{
//	    flow.BaseLibrary(),
//	    flow.StandardLibrary(),
//	    myProjectLibrary,
//	}
func BaseLibrary() action.Library {
	return action.Library{
		Name:        "flow/base",
		Description: "Small data and environment primitives for .nflow files",
		Actions:     utility.All(),
		Aliases: []action.Alias{
			{Canonical: "noop", Short: []string{"id", "identity", "pass"}},
			{Canonical: "debug", Short: []string{"dump", "print"}},
			{Canonical: "pick", Short: []string{"extract"}},
			{Canonical: "wrap", Short: []string{"box", "nest"}},
			{Canonical: "const", Short: []string{"literal"}},
			{Canonical: "fail", Short: []string{"boom"}},
			{Canonical: "env", Short: []string{"getenv"}},
			{Canonical: "uuid", Short: []string{"newid"}},
			{Canonical: "call", Short: []string{"invoke"}},
			{Canonical: "dispatch_by_prefix", Short: []string{"dispatch_prefix"}},
		},
	}
}
