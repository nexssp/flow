package progress_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

// ── progress.step ────────────────────────────────────────────────────

func TestStep_PassesThroughMapMinusMessage(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `{ x: 1, y: "z" } -> progress.step @{ message: "Loading" }`
	ex, err := runner.Execute(context.Background(), cfg, src, "step_map.nflow", nil)
	ktest.RequireNoError(t, err)

	out, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "output type = %T, want map", ex.Output)
	ktest.RequireEqual[any](t, out["x"], 1)
	ktest.RequireEqual[any](t, out["y"], "z")
	if _, hasMessage := out["message"]; hasMessage {
		t.Errorf("message key leaked into output: %#v", out)
	}
}

func TestStep_PassesThroughNonMapInput(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `runtime.const @{ value: "raw string" } -> progress.step @{ message: "After string" }`
	ex, err := runner.Execute(context.Background(), cfg, src, "step_string.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, ex.Output, "raw string")
}

func TestStep_UnwrapsRootSentinelForSliceInput(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `runtime.const @{ value: [1, 2, 3] } -> progress.step @{ message: "After slice" }`
	ex, err := runner.Execute(context.Background(), cfg, src, "step_slice.nflow", nil)
	ktest.RequireNoError(t, err)

	items, ok := ex.Output.([]any)
	ktest.RequireCondition(t, ok, "output type = %T, want []any", ex.Output)
	ktest.RequireLen(t, items, 3)
}

// ── progress.wrap ────────────────────────────────────────────────────

func TestWrap_SuccessDelegatesToTarget(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `{ x: 42 } -> progress.wrap @{ action: runtime.noop, message: "Running noop" }`
	ex, err := runner.Execute(context.Background(), cfg, src, "wrap_ok.nflow", nil)
	ktest.RequireNoError(t, err)

	out, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "output type = %T, want map", ex.Output)
	ktest.RequireEqual[any](t, out["x"], 42)
	if _, hasAction := out["action"]; hasAction {
		t.Errorf("action key leaked into output: %#v", out)
	}
	if _, hasMessage := out["message"]; hasMessage {
		t.Errorf("message key leaked into output: %#v", out)
	}
}

func TestWrap_PropagatesTargetError(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `{ x: 1 } -> progress.wrap @{ action: runtime.fail, message: "Will fail" }`
	_, err = runner.Execute(context.Background(), cfg, src, "wrap_err.nflow", nil)
	ktest.RequireCondition(t, err != nil, "target failure must propagate")
}

func TestWrap_DefaultMessageUsesTargetName(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	// No :message= is provided; the default is the target's registered
	// name (runtime.noop). The behavior is observable only through the
	// reporter output, so this test only asserts the pipeline succeeds
	// and no "message" key leaks into the result.
	src := `{ x: 1 } -> progress.wrap @{ action: runtime.noop }`
	ex, err := runner.Execute(context.Background(), cfg, src, "wrap_default.nflow", nil)
	ktest.RequireNoError(t, err)

	out, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "output type = %T, want map", ex.Output)
	ktest.RequireEqual[any](t, out["x"], 1)
	if _, hasMessage := out["message"]; hasMessage {
		t.Errorf("message key leaked into output: %#v", out)
	}
}

func TestWrap_UnwrapsRootSentinelForSliceInput(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `runtime.const @{ value: [1, 2, 3] } -> progress.wrap @{ action: runtime.noop, message: "Wrapping slice" }`
	ex, err := runner.Execute(context.Background(), cfg, src, "wrap_slice.nflow", nil)
	ktest.RequireNoError(t, err)

	items, ok := ex.Output.([]any)
	ktest.RequireCondition(t, ok, "output type = %T, want []any", ex.Output)
	ktest.RequireLen(t, items, 3)
}

func TestWrap_UnknownTargetFailsCompile(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `{ x: 1 } -> progress.wrap @{ action: does.not.exist }`
	_, err = runner.Execute(context.Background(), cfg, src, "wrap_unknown.nflow", nil)
	ktest.RequireCondition(t, err != nil, "unknown target must fail")
	ktest.RequireStringContains(t, err.Error(), "unknown capability")
}

func TestWrap_QuotedTargetFailsCompile(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	// ArgSchemas declares `action` as a capability reference. A quoted
	// string is a compile error.
	src := `{ x: 1 } -> progress.wrap @{ action: "runtime.noop" }`
	_, err = runner.Execute(context.Background(), cfg, src, "wrap_quoted.nflow", nil)
	ktest.RequireCondition(t, err != nil, "quoted capability ref must fail at compile")
	ktest.RequireStringContains(t, err.Error(), "bare capability reference")
}

func TestWrap_MissingActionFieldFailsCompile(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	// `action` is a required capability reference in ArgSchemas. A wrap
	// without it must be rejected at compile time with a clear message.
	src := `{ x: 1 } -> progress.wrap @{ message: "no target" }`
	_, err = runner.Execute(context.Background(), cfg, src, "wrap_no_action.nflow", nil)
	ktest.RequireCondition(t, err != nil, "missing action field must fail")
	ktest.RequireStringContains(t, err.Error(), "required capability reference")
}
