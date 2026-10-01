package runtime

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Base Library",
			Features: []core.SelfTestFeature{
				{Name: "noop", DSL: "@assert: result.x == 1\n{ x: 1 } -> noop"},
				{Name: "debug", DSL: `@assert: result == "visible"` + "\n" + `const @{ value: "visible" } -> debug`},
				{Name: "pick", DSL: `@assert: result == "alice@example.test"` + "\n" + `{ user: { email: "alice@example.test" } } -> pick @{ field: "user.email" }`},
				{Name: "wrap", DSL: `@assert: result.payload.value == 7` + "\n" + `{ value: 7 } -> wrap @{ key: "payload" }`},
				{Name: "const int coercion", DSL: "@assert: result == 42\nconst @{ value: 42 }"},
				{Name: "const bool coercion", DSL: "@assert: result == true\nconst @{ value: true }"},
				{Name: "fail with kind", DSL: `@assert: result == "recovered"` + "\n" + `fail @{ message: "boom", kind: "Timeout" } || const @{ value: "recovered" }`},
				{Name: "env", DSL: `@assert: result != ""` + "\n" + `const @{ value: "PATH" } -> env`},
				{Name: "uuid", DSL: "@assert: result != nil\nuuid"},
				{Name: "call", DSL: `@assert: result == "called"` + "\n" + `{ name: "const", payload: { value: "called" } } -> call`},
				{Name: "dispatch_by_prefix", DSL: `@assert: result.value == "x"` + "\n" + `{ prefix: "cov", key: "echo", payload: { value: "x" } } -> dispatch_by_prefix`},
			},
		},
	}
}
