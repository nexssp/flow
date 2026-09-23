package utility

import "github.com/nexssp/kernel/action"

// All returns every action in the utility sub-package in registration
// order. BaseLibrary consumes this directly.
func All() []action.AnyAction {
	return []action.AnyAction{
		NewNoopAction(),
		NewDebugAction(),
		NewPickAction(),
		NewWrapAction(),
		NewConstAction(),
		NewFailAction(),
		NewEnvAction(),
		NewUUIDAction(),
		NewCallAction(),
		NewDispatchByPrefixAction(),
	}
}
