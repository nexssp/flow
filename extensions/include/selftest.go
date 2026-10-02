package include

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Include",
			Features: []core.SelfTestFeature{
				{
					Name: "@include merges pipelines",
					DSL: `@include ./inc.nflow
@assert: result == "included"
pipeline.included_flow`,
					Files: map[string]string{
						"inc.nflow": `@pipeline included_flow
  runtime.const @{ value: "included" }
@end
`,
					},
				},
			},
		},
	}
}
