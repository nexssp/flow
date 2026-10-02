package macros

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Macros",
			Features: []core.SelfTestFeature{
				{
					Name: "basic expansion",
					DSL: `@macro hi() {
  runtime.const @{ value: "hi" }
}
@assert: result == "hi"
@hi()`,
				},
				{
					Name: "string argument",
					DSL: `@macro echo(v) {
  runtime.const @{ value: $v }
}
@assert: result == "hello"
@echo("hello")`,
				},
				{
					Name: "number argument",
					DSL: `@macro echo(v) {
  runtime.const @{ value: $v }
}
@assert: result == 42
@echo(42)`,
				},
				{
					Name: "multiple arguments",
					DSL: `@macro pair(k, v) {
  runtime.const @{ value: { key: $k, value: $v } }
}
@assert: result.key == "id"
@assert: result.value == 42
@pair("id", 42)`,
				},
				{
					Name: "compose atoms",
					DSL: `@macro seq(a, b) {
  $a -> $b
}
@assert: result.x == 1
{ x: 1 } -> @seq(runtime.noop, runtime.noop)`,
				},
				{
					Name: "nested call",
					DSL: `@macro echo(v) {
  runtime.const @{ value: $v }
}
@macro seq(a, b) {
  $a -> $b
}
@assert: result == "inner"
@seq(@echo("inner"), runtime.noop)`,
				},
				{
					Name: "comma inside quotes",
					DSL: `@macro echo(v) {
  runtime.const @{ value: $v }
}
@assert: result == "a,b"
@echo("a,b")`,
				},
				{
					Name: "macro inside @pipeline",
					DSL: `@macro g() {
  runtime.const @{ value: "g" }
}
@pipeline wrap_g
  @g()
@end
@assert: result == "g"
pipeline.wrap_g`,
				},
			},
		},
	}
}
