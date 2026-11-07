package runner_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestReservedPayloadFields_EndToEnd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		source    string
		assertion string
	}{
		{"projection", `{ count: 5, type: "invoice", len: 2, min: 3, user: { name: "Ada", count: 5 } } -> { ok: .count > 4 && .type == "invoice" && .len > 0 && .min < 10, name: .user.name, nested: .user.count, builtin: len([1,2]) }`, `result.ok && result.name == "Ada" && result.nested == 5 && result.builtin == 2`},
		{"assert", `{ count: 5, type: "invoice", len: 2, min: 3 } -> assert(.count > 4 && .type == "invoice" && .len > 0 && .min < 10, "reserved fields")`, `result.count == 5`},
		{"loop", `{ count: 0 } -> loop({ count: .count + 1 }) until(.count > 4)`, `result.count == 5`},
		{"match", `{ count: 5 } -> match { .count > 4 -> "yes", _ -> "no" }`, `result == "yes"`},
		{"composed projection", `{ count: 5 } -> { ok: .count > 4, child: race { { value: .count } } }`, `result.ok && result.child.value == 5`},
		{"keyword keys", `{ "true": 7, "and": 8 } -> { a: .true, b: .and }`, `result.a == 7 && result.b == 8`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := runner.BuildConfig(native.Bundles())
			if err != nil {
				t.Fatal(err)
			}
			execution, err := runner.Execute(t.Context(), cfg, tc.source, "reserved-fields.nflow", nil)
			if err != nil {
				t.Fatal(err)
			}
			if failures := runner.EvaluateAssertions(execution.Output, []string{tc.assertion}); len(failures) != 0 {
				t.Fatalf("assertion failed: %v (output %#v)", failures, execution.Output)
			}
		})
	}
}

func TestBareBuiltinDiagnostics_CompileBeforeExecution(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`assert(count > 4, "missing dot")`,
		`loop({ count: 5 }) until(count > 4)`,
		`match { count > 4 -> "yes", _ -> "no" }`,
		`{ ok: count > 4 }`,
		`{ child: race { "ok" }, ok: count > 4 }`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			cfg, err := runner.BuildConfig(native.Bundles())
			if err != nil {
				t.Fatal(err)
			}
			_, err = runner.Compile(t.Context(), cfg, source, "bare-builtin.nflow")
			if err == nil || !strings.Contains(err.Error(), "for a payload field use .count") {
				t.Fatalf("compile = %v, want field hint before execution", err)
			}
		})
	}
}

func TestLoopStopsWhenTerminationConditionFails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		until string
	}{
		{name: "evaluation error", until: "len(.denominator) > 0"},
		{name: "non-boolean result", until: "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var bodyCalls atomic.Int32
			body := action.New("test.bump", func(_ context.Context, input any) (any, error) {
				bodyCalls.Add(1)
				return input, nil
			}).Build()
			cfg, err := runner.BuildConfig(append(native.Bundles(), compiler.Bundle{
				ID:        "loop-condition-regression",
				Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{body}}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			source := "loop(test.bump) until(" + tc.until + ")"
			_, err = runner.Execute(t.Context(), cfg, source, "loop-condition.nflow", map[string]any{"denominator": 0})
			if err == nil || xerr.KindFrom(err) != xerr.KindValidation {
				t.Fatalf("Execute() error = %v, want validation error", err)
			}
			if got := bodyCalls.Load(); got != 1 {
				t.Fatalf("loop body called %d times after invalid termination condition, want exactly once", got)
			}
		})
	}
}
