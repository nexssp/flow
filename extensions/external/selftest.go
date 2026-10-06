package external

import (
	"runtime"

	"github.com/nexssp/flow/core"
)

func selftest() []core.SelfTestSection {
	jsonCommand := `echo {\"name\":\"Maksymilian\"}`
	if runtime.GOOS == "windows" {
		jsonCommand = `echo {"name":"Maksymilian"}`
	}
	jsonDSL := `@assert: result.output.name == "Maksymilian"
external.exec @{ cmd: ` + "`" + jsonCommand + "`" + ` }`

	return []core.SelfTestSection{
		{
			Name: "External",
			Features: []core.SelfTestFeature{
				{
					Name: "exec echo",
					DSL: `@assert: result.ok == true
external.exec @{ cmd: "echo hello" }`,
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
