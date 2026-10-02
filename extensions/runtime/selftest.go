package runtime

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Base Library",
			Features: []core.SelfTestFeature{
				{Name: "runtime.noop", DSL: "@assert: result.x == 1\n{ x: 1 } -> runtime.noop"},
				{Name: "runtime.debug", DSL: `@assert: result == "visible"` + "\n" + `runtime.const @{ value: "visible" } -> runtime.debug`},
				{Name: "runtime.pick", DSL: `@assert: result == "alice@example.test"` + "\n" + `{ user: { email: "alice@example.test" } } -> runtime.pick @{ field: "user.email" }`},
				{Name: "runtime.wrap", DSL: `@assert: result.payload.value == 7` + "\n" + `{ value: 7 } -> runtime.wrap @{ key: "payload" }`},
				{Name: "runtime.const int coercion", DSL: "@assert: result == 42\nruntime.const @{ value: 42 }"},
				{Name: "runtime.const bool coercion", DSL: "@assert: result == true\nruntime.const @{ value: true }"},
				{Name: "runtime.fail with kind", DSL: `@assert: result == "recovered"` + "\n" + `runtime.fail @{ message: "boom", kind: "Timeout" } || runtime.const @{ value: "recovered" }`},
				{Name: "runtime.env", DSL: `@assert: result != ""` + "\n" + `runtime.const @{ value: "PATH" } -> runtime.env`},
				{Name: "runtime.uuid", DSL: "@assert: result != nil\nruntime.uuid"},
				{Name: "runtime.call", DSL: `@assert: result == "called"` + "\n" + `{ name: "runtime.const", payload: { value: "called" } } -> runtime.call`},
				{Name: "runtime.dispatch_by_prefix", DSL: `@assert: result.value == "x"` + "\n" + `{ prefix: "cov", key: "echo", payload: { value: "x" } } -> runtime.dispatch_by_prefix`},
			},
		},
	}
}
