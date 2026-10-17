package projection_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestProjectionControlValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		assert string
	}{
		{"deep nesting", `{ peak: 98 } -> { obj: { values: [race { { result: match(.peak) { >= 98 -> "high", _ -> "low" } } }] } }`, `result.obj.values[0].result == "high"`},
		{"lazy ternary", `{ value: false ? race { runtime.fail @{ message: "must not run" } } : "skipped" }`, `result.value == "skipped"`},
		{"lazy coalescing", `{ value: "present" ?? race { runtime.fail @{ message: "must not run" } } }`, `result.value == "present"`},
		{"strings and keys", `{ race: "race { ignored }", match: "match(.x) { ignored }", value: race { "ok" } }`, `result.race == "race { ignored }" && result.value == "ok"`},
		{"internal names cannot collide", `{ __flow_projection_context__: "user", value: race { "ok" } }`, `result.__flow_projection_context__ == "user" && result.value == "ok"`},
		{"scalar input", `42 -> { copy: race { runtime.noop }, category: match(.) { >= 42 -> "high", _ -> "low" } }`, `result.copy == 42 && result.category == "high"`},
		{"spread and input scope", `{ value: 10 } -> { ..., value: 20, nested: parallel { a: { copy: .value }, b: { deeper: race { { copy: .value } } } } }`, `result.value == 20 && result.nested.a.copy == 10 && result.nested.b.deeper.copy == 10`},
		{"control policy modifiers", `{ value: race { "ok" }:timeout=1s }`, `result.value == "ok"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := runner.BuildConfig(native.Bundles())
			ktest.RequireNoError(t, err)
			execution, err := runner.Execute(t.Context(), cfg, tc.source, "control-values.nflow", nil)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, []string{tc.assert})), 0)
		})
	}
}

func TestProjectionControlCompilationErrors(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	for _, source := range []string{
		`{ value: parallel { a: "one", a: "two" } }`,
		`{ value: parallel {} }`,
		`{ value: race {} }`,
		`{ value: race { missing.capability } }`,
		`{ value: match(.peak) { > -> "bad", _ -> "ok" } }`,
	} {
		_, compileErr := runner.Compile(t.Context(), cfg, source, "invalid-values.nflow")
		ktest.RequireCondition(t, compileErr != nil, "invalid embedded Flow compiled: %s", source)
	}
	_, err = runner.Compile(t.Context(), cfg, "{\n value: parallel { a: \"one\", a: \"two\" }\n}", "duplicate.nflow")
	ktest.RequireErrorContains(t, err, "duplicate.nflow:2:")
}

func TestProjectionControlProgramConcurrentReuse(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	compiled, err := runner.Compile(t.Context(), cfg,
		`{ copy: parallel { a: { id: .id }, b: race { { id: .id } } }, category: match(.id) { >= 0 -> "nonnegative", _ -> "negative" } }`, "concurrent-values.nflow")
	ktest.RequireNoError(t, err)
	for i := range 24 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			out, runErr := action.InvokeAny(t.Context(), compiled.Program, map[string]any{"id": i})
			ktest.RequireNoError(t, runErr)
			checks := []string{`result.copy.a.id == ` + strconv.Itoa(i), `result.copy.b.id == ` + strconv.Itoa(i), `result.category == "nonnegative"`}
			ktest.RequireEqual(t, len(runner.EvaluateAssertions(out, checks)), 0)
		})
	}
}

func TestProjectionRaceCancelsAndJoinsLoser(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	exited := make(chan struct{})
	winner := action.New("test.winner", func(ctx context.Context, _ any) (any, error) {
		select {
		case <-started:
			return "winner", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}).Build()
	loser := action.New("test.loser", func(ctx context.Context, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	}).Build()
	cfg, err := runner.BuildConfig(append(native.Bundles(), compiler.Bundle{
		ID: "projection-test", Libraries: []action.Library{{Name: "projection-test", Actions: []action.AnyAction{winner, loser}}},
	}))
	ktest.RequireNoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	execution, err := runner.Execute(ctx, cfg, `{ value: race { test.winner, test.loser } }`, "race-values.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, []string{`result.value == "winner"`})), 0)
	select {
	case <-exited:
	default:
		t.Fatal("projection returned before its canceled race candidate exited")
	}
}

func TestProjectionControlCanceledInvocation(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	compiled, err := runner.Compile(t.Context(), cfg, `{ value: race { "ok" } }`, "canceled-values.nflow")
	ktest.RequireNoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = action.InvokeAny(ctx, compiled.Program, nil)
	ktest.RequireCondition(t, errors.Is(err, context.Canceled), "want caller cancellation, got %v", err)
}

func TestProjectionPreservesWrappedAppError(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	compiled, err := runner.Compile(t.Context(), cfg, `{ value: race { runtime.fail @{ kind: "Unavailable", message: "upstream failed" } } }`, "app-error-values.nflow")
	ktest.RequireNoError(t, err)
	_, err = action.InvokeAny(t.Context(), compiled.Program, nil)
	ktest.RequireCondition(t, err != nil, "expected projection failure")
	appErr, ok := errors.AsType[*xerr.AppError](err)
	ktest.RequireCondition(t, ok && appErr != nil, "want wrapped AppError, got %T: %v", err, err)
	ktest.RequireEqual(t, appErr.Kind, xerr.KindUnavailable)
}
