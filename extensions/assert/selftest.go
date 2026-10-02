package assert

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Operators",
			Features: []core.SelfTestFeature{
				{
					Name: "Assert assert(...)",
					DSL: `@assert: result.score == 95
{ score: 95 } -> assert(.score >= 50, "score must be at least 50") -> runtime.noop`,
				},
			},
		},
		{
			Name: "Directives",
			Features: []core.SelfTestFeature{
				{
					Name: "@assert (single + multiple)",
					DSL: `@assert: result == "ok"
@assert: 1 + 1 == 2
runtime.const @{ value: "ok" }`,
				},
			},
		},
	}
}
