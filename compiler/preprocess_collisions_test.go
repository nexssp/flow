package compiler_test

import (
	"testing"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/builtin"

	"github.com/nexssp/flow/compiler"
)

func TestPreprocessDots_AllInstalledBuiltins(t *testing.T) {
	t.Parallel()
	for _, name := range builtin.Names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, source := range []string{"." + name + " == 7", "." + name + ".value == 7", ".user." + name + " == 7"} {
				input := map[string]any{name: 7, "user": map[string]any{name: 7}}
				if source == "."+name+".value == 7" {
					input[name] = map[string]any{"value": 7}
				}
				assertDotExpression(t, source, input, true)
			}
		})
	}
}

func TestPreprocessDots_ReservedKeywords(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"true", "false", "nil", "not", "in", "or", "and", "matches", "contains", "startsWith", "endsWith", "let", "if", "else", "__root__"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertDotExpression(t, "."+name+" == 7", map[string]any{name: 7}, true)
		})
	}
}

func TestPreprocessDots_CollisionCompatibility(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"count": 5, "type": "invoice", "len": 2, "min": 3, "max": 8,
		"counted": 9, "name": "Ada", "user": map[string]any{"name": "Ada", "count": 5},
		"rows": []any{map[string]any{"count": 5}},
	}
	for _, source := range []string{
		`.count > 4`, `.type == "invoice"`, `.len > 0`, `.min < 10`, `.max == 8`,
		`.user.name == "Ada"`, `.name == "Ada"`, `.user.count == 5`, `.user?.count == 5`,
		`.counted == 9`, `len(.rows) == 1`, `all(.rows, #.count > 4)`,
		`count(.rows, #.count > 4) == 1`, `min(3, 8) == .min`,
		`".count" == '.count'`, "`.len` == '.len'",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertDotExpression(t, source, input, true)
		})
	}
	for source, want := range map[string]string{
		`.count`: `__root__.count`, `.count.value`: `__root__.count.value`,
		`.user.count`: `user.count`, `.counted`: `counted`, `.user.name`: `user.name`,
		`len(.name)`: `len(name)`, `#.count`: `#.count`, `.user?.count`: `user?.count`,
	} {
		if got := compiler.PreprocessDots(source); got != want {
			t.Errorf("PreprocessDots(%q) = %q, want %q", source, got, want)
		}
	}
}

func assertDotExpression(t *testing.T, source string, input map[string]any, want any) {
	t.Helper()
	program, err := compiler.CompileExpression(compiler.PreprocessDots(source), expr.AllowUndefinedVariables())
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	got, err := expr.Run(program, compiler.BuildEnv(input))
	if err != nil {
		t.Fatalf("run %q: %v", source, err)
	}
	if got != want {
		t.Fatalf("run %q = %#v, want %#v", source, got, want)
	}
}
