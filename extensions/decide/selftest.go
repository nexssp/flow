package decide

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Decide",
			Features: []core.SelfTestFeature{
				{
					Name: "decide requires a registered backend",
					DSL: `@assert: result == "recovered"
{ backend: "missing", questions: { x: { type: "label" } } } -> decide.run
|| runtime.const @{ value: "recovered" }`,
				},
			},
		},
	}
}
