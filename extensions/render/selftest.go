package render

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Stream: Render",
			Features: []core.SelfTestFeature{
				{
					Name: "render.markdown",
					DSL:  `@assert: len(result) >= 1` + "\n" + `fs.walk -> fs.filter:ext="txt" -> fs.read -> render.markdown:editor="markdown" -> collect`,
					Files: map[string]string{
						"a.txt": "hello",
					},
				},
			},
		},
	}
}
