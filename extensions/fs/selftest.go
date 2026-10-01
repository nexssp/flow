package fs

import "github.com/nexssp/flow/core"

const fixtureTxt = "a.txt"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Stream: FS",
			Features: []core.SelfTestFeature{
				{
					Name: "collect",
					DSL:  "@assert: len(result) == 3\ncov.items -> collect",
				},
				{
					Name: "fs.walk",
					DSL:  "@assert: len(result) > 0\nfs.walk -> collect",
					Files: map[string]string{
						fixtureTxt:  "hello",
						"sub/b.txt": "world",
					},
				},
				{
					Name: "fs.filter ext",
					DSL:  "@assert: len(result) >= 1\nfs.walk -> fs.filter:ext=\"txt\" -> collect",
					Files: map[string]string{
						fixtureTxt: "hello",
						"b.md":     "world",
					},
				},
				{
					Name: "fs.read",
					DSL:  "@assert: len(result) >= 1\nfs.walk -> fs.filter:ext=\"txt\" -> fs.read -> collect",
					Files: map[string]string{
						fixtureTxt: "hello",
					},
				},
				{
					Name: "fs.sort",
					DSL:  "@assert: len(result) >= 1\nfs.walk -> fs.sort:by=\"rel_path\" -> collect",
					Files: map[string]string{
						fixtureTxt: "x",
						"b.txt":    "y",
					},
				},
				{
					Name: "out.stdout",
					DSL:  "@assert: result != nil\nfs.walk -> out.stdout",
					Files: map[string]string{
						fixtureTxt: "x",
					},
				},
				{
					Name: "out.file",
					DSL: `@assert: result != nil
fs.walk -> fs.filter:ext="txt" -> fs.read -> out.file:path="out.txt"`,
					Files: map[string]string{
						"input.txt": "saved content",
					},
				},
			},
		},
	}
}
