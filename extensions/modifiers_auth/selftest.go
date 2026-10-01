package modifiers_auth

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Modifiers",
			Features: []core.SelfTestFeature{
				{
					Name: "Modifier :auth",
					DSL: `const:auth @{ value: "ok" }
@assert: result == "ok"`,
				},
				{
					Name: "Modifier :role=",
					DSL: `const:role=admin @{ value: "ok" }
@assert: result == "ok"`,
				},
				{
					Name: "Modifier :perm=",
					DSL: `const:perm=read @{ value: "ok" }
@assert: result == "ok"`,
				},
				{
					Name: "Modifier :feature=",
					DSL: `const:feature=test.flag @{ value: "ok" }
@assert: result == "ok"`,
				},
			},
		},
	}
}
