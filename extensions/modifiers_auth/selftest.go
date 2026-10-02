package modifiers_auth

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Modifiers",
			Features: []core.SelfTestFeature{
				{
					Name: "Modifier :auth",
					DSL: `runtime.const:auth @{ value: "ok" }
@assert: result == "ok"`,
				},
				{
					Name: "Modifier :role=",
					DSL: `runtime.const:role=admin @{ value: "ok" }
@assert: result == "ok"`,
				},
				{
					Name: "Modifier :perm=",
					DSL: `runtime.const:perm=read @{ value: "ok" }
@assert: result == "ok"`,
				},
				{
					Name: "Modifier :feature=",
					DSL: `runtime.const:feature=test.flag @{ value: "ok" }
@assert: result == "ok"`,
				},
			},
		},
	}
}
