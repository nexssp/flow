package macros_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow/cli"
)

const requireLine = "@require github.com/nexssp/flow/extensions/macros\n\n"

// TestMacroEngineEndToEnd runs each macro scenario as an independent
// compilation. Every subtest has one @macro declaration, one
// invocation, and one @assert on the top-level result. This proves the
// engine end-to-end without relying on parallel-composition semantics.
func TestMacroEngineEndToEnd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			"no_args",
			requireLine + `
@macro hi() {
  const @{ value: "hi" }
}
@assert: result == "hi"
@hi()`,
		},
		{
			"string_arg",
			requireLine + `
@macro echo(v) {
  const @{ value: $v }
}
@assert: result == "hello"
@echo("hello")`,
		},
		{
			"number_arg",
			requireLine + `
@macro echo(v) {
  const @{ value: $v }
}
@assert: result == 42
@echo(42)`,
		},
		{
			"two_args_object",
			requireLine + `
@macro pair(k, v) {
  const @{ value: { key: $k, value: $v } }
}
@assert: result.key == "id"
@assert: result.value == 42
@pair("id", 42)`,
		},
		{
			"compose_atoms",
			requireLine + `
@macro seq(a, b) {
  $a -> $b
}
@assert: result.x == 1
{ x: 1 } -> @seq(noop, noop)`,
		},
		{
			"nested_call",
			requireLine + `
@macro echo(v) {
  const @{ value: $v }
}
@macro seq(a, b) {
  $a -> $b
}
@assert: result == "inner"
@seq(@echo("inner"), noop)`,
		},
		{
			"comma_in_quoted_arg",
			requireLine + `
@macro echo(v) {
  const @{ value: $v }
}
@assert: result == "a,b"
@echo("a,b")`,
		},
		{
			"pipeline_body_sees_macros",
			requireLine + `
@macro greet() {
  const @{ value: "g" }
}
@pipeline wrap_greet
  @greet()
@end
@assert: result == "g"
wrap_greet`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if exit := cli.RunEmbedded(context.Background(), tc.src, nil); exit != 0 {
				t.Fatalf("case %q failed (exit=%d)", tc.name, exit)
			}
		})
	}
}
