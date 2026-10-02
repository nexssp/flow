package nodes_supervisor

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Nodes: Supervisor",
			Features: []core.SelfTestFeature{
				{
					Name: "supervisor",
					DSL: `@assert: result.succeeded == 1
{ tasks: [ { id: "t1", dsl: "runtime.noop", payload: { x: 1 } } ] } -> supervisor.run`,
				},
			},
		},
	}
}
