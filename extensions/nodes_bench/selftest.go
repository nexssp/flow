package nodes_bench

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Nodes: Bench",
			Features: []core.SelfTestFeature{
				{
					Name: "bench.run",
					DSL: `@assert: result.action == "runtime.noop"
@assert: result.iterations == 1
{ action: "noop", iterations: 1, payload: { value: "b" } } -> bench.run`,
				},
				{
					Name: "bench.save",
					DSL: `@assert: result.file == "bench.json"
{ file: "bench.json", action: "noop", iterations: 1, p95_ms: 10 } -> bench.save`,
					Files: map[string]string{
						"placeholder.txt": "",
					},
				},
			},
		},
	}
}
