package external

import (
	"runtime"

	"github.com/nexssp/flow/core"
)

func selftest() []core.SelfTestSection {
	jsonCommand := `echo {\"name\":\"Ada\"}`
	if runtime.GOOS == "windows" {
		jsonCommand = `echo {"name":"Ada"}`
	}
	jsonDSL := `@assert: result.output.name == "Ada"
exec @{ cmd: ` + "`" + jsonCommand + "`" + ` }`

	return []core.SelfTestSection{
		{
			Name: "External",
			Features: []core.SelfTestFeature{
				{
					Name: "exec echo",
					DSL: `@assert: result.ok == true
exec @{ cmd: "echo hello" }`,
				},
				{
					Name: "exec json stdout",
					DSL:  jsonDSL,
				},
				{
					Name: "wasm requires a module file",
					Skip: "needs a compiled .wasm fixture; exercised in library tests instead",
				},
			},
		},
	}
}
