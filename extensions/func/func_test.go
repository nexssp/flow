package funcs_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/description"
	funcext "github.com/nexssp/flow/extensions/func"
	"github.com/nexssp/flow/extensions/include"
	"github.com/nexssp/flow/extensions/loop"
	macroext "github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/extensions/match"
	"github.com/nexssp/flow/extensions/on_error"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func nativeConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]compiler.Bundle{
		assert.Bundle(nil),
		description.Bundle(nil),
		funcext.Bundle(nil),
		include.Bundle(nil),
		loop.Bundle(nil),
		macroext.Bundle(nil),
		match.Bundle(nil),
		on_error.Bundle(nil),
		pipeline.Bundle(nil),
		projection.Bundle(nil),
		runtime.Bundle(nil),
		schema.Bundle(nil),
		syntax.Bundle(nil),
	})
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	return cfg
}

func TestFuncFixtureUsesSchemaDefaultsAndCanBeReusedByPipelines(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(funcext.Bundle(nil).Fixtures, "nflows/reusable_shipping_fee.nflow")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fixtureDir := t.TempDir()
	cfg := nativeConfig(t)
	execution, err := runner.Execute(t.Context(), cfg, string(fixture), filepath.Join(fixtureDir, "reusable_shipping_fee.nflow"), nil)
	if err != nil {
		t.Fatalf("execute fixture: %v", err)
	}
	result, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	if got, want := result["subtotal_minor"], 3000; got != want {
		t.Errorf("default subtotal = %#v, want %d", got, want)
	}
	if got, want := result["shipping_minor"], 599; got != want {
		t.Errorf("shipping fee = %#v, want %d", got, want)
	}
}

func TestFuncSupportsForwardReferencesAndNestedCalls(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	source := `@func commerce.totalFee(subtotal int) int = commerce.baseFee(subtotal) + 5
@func commerce.baseFee(subtotal int) int = subtotal / 100
{ fee: commerce.totalFee(.subtotal) }`
	execution, err := runner.Execute(t.Context(), cfg, source, "forward-functions.nflow", map[string]any{"subtotal": 3000})
	if err != nil {
		t.Fatalf("execute forward-reference functions: %v", err)
	}
	result, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	if got, want := result["fee"], 35; got != want {
		t.Errorf("fee = %#v (%T), want %d", got, got, want)
	}
}

func TestFuncImportsComposeNamespacesAcrossNestedIncludes(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	root := t.TempDir()
	writeSource(t, root, "math.nflow", `@func math.double(value int) int = value * 2`)
	writeSource(t, root, "shipping.nflow", `@include "./math.nflow" AS shared
@func commerce.shippingFee(amount int) int = shared.math.double(amount) + 1`)

	source := `@include "./shipping.nflow" AS pricing
{ amount: .amount, category: match(.amount) { >= pricing.commerce.shippingFee(20) -> "high", _ -> "low" } }
@assert: pricing.commerce.shippingFee(result.amount) == 85`
	mainPath := filepath.Join(root, "main.nflow")
	execution, err := runner.Execute(t.Context(), cfg, source, mainPath, map[string]any{"amount": 42})
	if err != nil {
		t.Fatalf("execute imported-function condition: %v", err)
	}
	result, ok := execution.Output.(map[string]any)
	if !ok || result["category"] != "high" {
		t.Fatalf("output = %#v, want category high", execution.Output)
	}
	failures := runner.EvaluateAssertionsContextWithExpressionCompiler(
		t.Context(), execution.Output, []string{"pricing.commerce.shippingFee(result.amount) == 85"}, nil,
		compiler.ExpressionCompilerFromMeta(execution.Meta),
	)
	if len(failures) != 0 {
		t.Fatalf("imported function assertion failures: %v", failures)
	}

	argumentSource := `@include "./shipping.nflow" AS pricing
runtime.const @{ shipping: pricing.commerce.shippingFee(.amount) }`
	argumentExecution, err := runner.Execute(t.Context(), cfg, argumentSource, mainPath, map[string]any{"amount": 21})
	if err != nil {
		t.Fatalf("execute imported function in argument map: %v", err)
	}
	argumentResult, ok := argumentExecution.Output.(map[string]any)
	if !ok || argumentResult["shipping"] != 43 {
		t.Fatalf("argument-map output = %#v, want shipping 43", argumentExecution.Output)
	}
}

func writeSource(t *testing.T, root, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestFuncWorksInsideImportedTypedMacroExpression(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	root := t.TempDir()
	writeSource(t, root, "billing.nflow", `@func commerce.taxMinor(subtotal int) int = subtotal / 10
@macro passthrough(expression: Expr) {
  $expression
}`)

	source := `@include "./billing.nflow" AS billing
billing.@passthrough(runtime.const @{ value: billing.commerce.taxMinor(120) })`
	execution, err := runner.Execute(t.Context(), cfg, source, filepath.Join(root, "main.nflow"), nil)
	if err != nil {
		t.Fatalf("execute imported typed macro with imported function: %v", err)
	}
	if execution.Output != 12 {
		t.Fatalf("output = %#v, want 12", execution.Output)
	}
}

func TestFuncCallsInLoopAndOnErrorConditions(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	root := t.TempDir()
	writeSource(t, root, "conditions.nflow", `@func checks.done(value int) bool = value >= 3
@func checks.isTimeout(kind string) bool = kind == "Timeout"`)

	loopSource := `@include "./conditions.nflow" AS calc
{ count: 0 } -> loop({ count: .count + 1 }) until(calc.checks.done(.count))`
	loopResult, err := runner.Execute(t.Context(), cfg, loopSource, filepath.Join(root, "loop.nflow"), nil)
	if err != nil {
		t.Fatalf("execute function-based loop condition: %v", err)
	}
	loopOutput, ok := loopResult.Output.(map[string]any)
	if !ok || loopOutput["count"] != 3 {
		t.Fatalf("loop output = %#v, want count 3", loopResult.Output)
	}

	errorSource := `@include "./conditions.nflow" AS calc
@pipeline recover
  { recovered: true }
@end
@pipeline fallback
  { recovered: false }
@end
@on_error {
  when calc.checks.isTimeout(error.kind) -> pipeline.recover
  else -> pipeline.fallback
}
runtime.fail @{ kind: "Timeout", message: "test" }`
	errorResult, err := runner.Execute(t.Context(), cfg, errorSource, filepath.Join(root, "on-error.nflow"), nil)
	if err != nil {
		t.Fatalf("execute function-based @on_error condition: %v", err)
	}
	errorOutput, ok := errorResult.Output.(map[string]any)
	if !ok || errorOutput["recovered"] != true {
		t.Fatalf("on_error output = %#v, want recovered true", errorResult.Output)
	}
}

func TestFuncRuntimeIsSourceLocalAndSafeForConcurrentReuse(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	compile := func(source, name string) action.AnyAction {
		t.Helper()
		compiled, err := runner.Compile(t.Context(), cfg, source, name)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		return compiled.Program
	}
	first := compile("@func local.value() int = 1\n{ value: local.value() }", "first-local-func.nflow")
	second := compile("@func local.value() int = 2\n{ value: local.value() }", "second-local-func.nflow")

	for _, test := range []struct {
		name string
		prog action.AnyAction
		want int
	}{
		{name: "first", prog: first, want: 1},
		{name: "second", prog: second, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for range 8 {
				var wg sync.WaitGroup
				for range 4 {
					wg.Go(func() {
						output, err := action.InvokeAny(t.Context(), test.prog, nil)
						if err != nil {
							t.Errorf("invoke: %v", err)
							return
						}
						values, ok := output.(map[string]any)
						if !ok || values["value"] != test.want {
							t.Errorf("output = %#v, want value %d", output, test.want)
						}
					})
				}
				wg.Wait()
			}
		})
	}
}

func TestFuncCompileDiagnostics(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	cases := []struct {
		name     string
		src      string
		want     string
		wantLine int
	}{
		{
			name: "unsupported parameter type",
			src:  "@func commerce.bad(value any) int = 1\n{ value: commerce.bad(1) }",
			want: "unsupported parameter type",
		},
		{
			name: "bad result expression",
			src:  "@func commerce.bad(value int) string = value + 1\n{ value: commerce.bad(1) }",
			want: "@func commerce.bad",
		},
		{
			name: "unknown body variable",
			src:  "@func commerce.bad(value int) int = value + mystery\n{ value: commerce.bad(1) }",
			want: "@func commerce.bad",
		},
		{
			name: "recursion",
			src:  "@func commerce.loop(value int) int = commerce.loop(value)\n{ value: commerce.loop(1) }",
			want: "recursive function cycle",
		},
		{
			name:     "wrong arity",
			src:      "@func commerce.add(left int, right int) int = left + right\n{ value: commerce.add(1) }",
			want:     "argument",
			wantLine: 2,
		},
		{
			name:     "wrong argument type",
			src:      "@func commerce.add(left int, right int) int = left + right\n{ value: commerce.add(\"1\", 2) }",
			want:     "argument",
			wantLine: 2,
		},
		{
			name: "nondeterministic builtin",
			src:  "@func commerce.clock() int = int(now())\n{ value: commerce.clock() }",
			want: "now",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := runner.Compile(t.Context(), cfg, test.src, "invalid-function.nflow")
			if err == nil {
				t.Fatal("compile succeeded; expected a diagnostic")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			wantLine := test.wantLine
			if wantLine == 0 {
				wantLine = 1
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("invalid-function.nflow:%d:", wantLine)) {
				t.Errorf("diagnostic lost declaration location: %v", err)
			}
		})
	}
}

func TestFuncIntParameterAcceptsIntegralJSONNumbersOnly(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	source := "@func local.twice(value int) int = value * 2\n{ value: local.twice(.value) }"
	valid, err := runner.Execute(t.Context(), cfg, source, "numeric-function.nflow", map[string]any{"value": float64(21)})
	if err != nil {
		t.Fatalf("execute with integral JSON number: %v", err)
	}
	values, ok := valid.Output.(map[string]any)
	if !ok || values["value"] != 42 {
		t.Fatalf("output = %#v, want value 42", valid.Output)
	}
	_, err = runner.Execute(t.Context(), cfg, source, "fractional-function.nflow", map[string]any{"value": 21.5})
	if err == nil || !strings.Contains(err.Error(), `parameter "value"`) {
		t.Fatalf("fractional input error = %v, want integer parameter diagnostic", err)
	}
}

func TestFuncRejectsDuplicateDeclarationsAndCrossSourceShadowing(t *testing.T) {
	t.Parallel()
	cfg := nativeConfig(t)
	duplicate := "@func commerce.value() int = 1\n@func commerce.value() int = 2\n{ value: commerce.value() }"
	if _, err := runner.Compile(t.Context(), cfg, duplicate, "duplicate-func.nflow"); err == nil || !strings.Contains(err.Error(), "duplicate declaration") {
		t.Fatalf("duplicate function error = %v, want duplicate declaration", err)
	}

	inherited := "@func commerce.value() int = 1\n@pipeline child\n  @func commerce.value() int = 2\n  { value: commerce.value() }\n@end\n{} -> pipeline.child"
	if _, err := runner.Compile(t.Context(), cfg, inherited, "shadowed-func.nflow"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("inherited duplicate function error = %v, want duplicate", err)
	}
}
