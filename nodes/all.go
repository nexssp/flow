package nodes

import "github.com/nexssp/kernel/action"

// All returns every domain-neutral action this package provides.
//
// Used by flow.Library() so a runtime that only needs the flow DSL can
// get a working action set without pulling in AI, sandbox, or provider
// dependencies.
func All() []action.AnyAction {
	return []action.AnyAction{
		NewLogInfoAction(),
		NewLogWarnAction(),
		NewLogErrorAction(),

		NewBenchRunAction(),
		NewBenchSaveAction(),
		NewBenchCompareAction(),

		NewDistributeMapAction(),
		NewDistributeReduceAction(),

		NewSupervisorNode("supervisor"),
	}
}
