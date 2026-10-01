// Package runtime ships the primitive actions every .nflow pipeline
// starts from: const, noop, debug, fail, pick, wrap, with, env, uuid,
// call, dispatch_by_prefix, json.clean. This is a native bundle —
// always mounted by native.Bundles().
//
// Typical use:
//
//	{ user_id: 42 } -> pick @{ field: "user_id" } -> wrap @{ key: "data" }
//	fail @{ kind: "Timeout", message: "upstream" } || const @{ value: "fallback" }
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
		SelfTest:  selftest,
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
			{Canonical: "with", Short: []string{"merge", "update"}},
			{Canonical: "sleep", Short: []string{"delay", "wait"}},
		},
	}
}
