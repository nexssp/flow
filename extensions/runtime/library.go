// Package runtime ships the primitive actions every .nflow pipeline
// starts from: runtime.const, runtime.noop, runtime.debug, runtime.fail,
// runtime.pick, runtime.wrap, runtime.with, runtime.env, runtime.uuid,
// runtime.call, runtime.dispatch_by_prefix, json.clean, runtime.sleep. This is a native bundle —
// always mounted by native.Bundles().
//
// Typical use:
//
//	{ user_id: 42 } -> runtime.pick @{ field: "user_id" } -> runtime.wrap @{ key: "data" }
//	runtime.fail @{ kind: "Timeout", message: "upstream" } || runtime.const @{ value: "fallback" }
package runtime

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "runtime"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{library()},
		ArgSchemas: map[string][]core.ArgFieldSpec{
			"runtime.call": {{Name: "name", Kind: core.ArgCapabilityRef}},
		},
		SelfTest: selftest,
	}
}

// library returns the runtime actions as an action.Library. This is
// the single place that lists every primitive — add a new one here.
func library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			Const,
			Debug,
			Noop,
			Fail,
			Wrap,
			Pick,
			With,
			Env,
			UUID,
			Call,
			DispatchByPrefix,
			JSONClean,
			Sleep,
		},
	}
}
