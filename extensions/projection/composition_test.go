package projection_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestProjectionShorthandForms(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	cases := []struct {
		name   string
		source string
		assert []string
	}{
		{
			name:   "bare-only",
			source: `{ name: "Ada", email: "ada@example.test", extra: "discard" } -> { name, email }`,
			assert: []string{`result.name == "Ada"`, `result.email == "ada@example.test"`, `!("extra" in result)`},
		},
		{
			name:   "explicit-only",
			source: `{ name: "Ada", email: "ada@example.test" } -> { display: .name, address: .email }`,
			assert: []string{`result.display == "Ada"`, `result.address == "ada@example.test"`},
		},
		{
			name:   "mixed",
			source: `{ name: "Ada", email: "ada@example.test", canbehere: { everything: "nested" }, another: "copied", canbe: { something: "explicit" } } -> { name, email, somethingelse: .canbehere.everything, another, other: .canbe.something }`,
			assert: []string{`result.name == "Ada"`, `result.email == "ada@example.test"`, `result.somethingelse == "nested"`, `result.another == "copied"`, `result.other == "explicit"`},
		},
		{
			name:   "object literal stays explicit",
			source: `{ name: "Ada", email: "ada@example.test" }`,
			assert: []string{`result.name == "Ada"`, `result.email == "ada@example.test"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			execution, execErr := runner.Execute(t.Context(), cfg, tc.source, "projection-shorthand.nflow", nil)
			ktest.RequireNoError(t, execErr)
			ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, tc.assert)), 0)
		})
	}
	execution, err := runner.Execute(t.Context(), cfg,
		`{ name: "Ada" } -> { name }`, "argument-map-shorthand.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, []string{`result.name == "Ada"`})), 0)
}

func TestProjectionUsesCompileTimeConfigInExpressions(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	simpleSource := "@config:base=\"/srv\"\n{ path: @config.base + \"/\" + .path }"
	simple, err := runner.Execute(t.Context(), cfg, simpleSource, "config-expression.nflow", map[string]any{"path": "tmp"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(runner.EvaluateAssertions(simple.Output, []string{`result.path == "/srv/tmp"`})), 0)

	composedSource := "@config:base=\"/srv\"\n@config:label=\"ok\"\n" +
		`{ path: @config.base + "/" + .path, ready: match(@config.label) { @config.label -> "ok", _ -> "no" }, active: if (@config.label == "ok") { "yes" } else { "no" } }`
	composed, err := runner.Execute(t.Context(), cfg, composedSource, "composed-config-expression.nflow", map[string]any{"path": "tmp"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(runner.EvaluateAssertions(composed.Output, []string{
		`result.path == "/srv/tmp"`, `result.ready == "ok"`, `result.active == "yes"`,
	})), 0)

	_, err = runner.Compile(t.Context(), cfg,
		"@config:base=\"/srv\"\n"+`{ path: @config.missing + "/" + .path }`,
		"unknown-config-expression.nflow",
	)
	ktest.RequireErrorContains(t, err, "unknown @config.missing")
}

func TestProjectionControlValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		assert string
	}{
		{"deep nesting", `{ peak: 98 } -> { obj: { values: [race { { result: match(.peak) { >= 98 -> "high", _ -> "low" } } }] } }`, `result.obj.values[0].result == "high"`},
		{"lazy ternary", `{ value: false ? race { runtime.fail ( message: "must not run" ) } : "skipped" }`, `result.value == "skipped"`},
		{"lazy coalescing", `{ value: "present" ?? race { runtime.fail ( message: "must not run" ) } }`, `result.value == "present"`},
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
	compiled, err := runner.Compile(t.Context(), cfg, `{ value: race { runtime.fail ( kind: "Unavailable", message: "upstream failed" ) } }`, "app-error-values.nflow")
	ktest.RequireNoError(t, err)
	_, err = action.InvokeAny(t.Context(), compiled.Program, nil)
	ktest.RequireCondition(t, err != nil, "expected projection failure")
	appErr, ok := errors.AsType[*xerr.AppError](err)
	ktest.RequireCondition(t, ok && appErr != nil, "want wrapped AppError, got %T: %v", err, err)
	ktest.RequireEqual(t, appErr.Kind, xerr.KindUnavailable)
}

func TestProjectionRuntimeConflictPreflightDoesNotExposeValues(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	_, err = runner.Execute(t.Context(), cfg,
		`noop as tool -> { result: (tool.task) }`,
		"projection-shadowing-runtime.nflow",
		map[string]any{"tool": map[string]any{"task": "payload-secret"}, "task": "binding-secret"},
	)
	ktest.RequireCondition(t, xerr.KindFrom(err) == xerr.KindConflict, "want Conflict, got %v", err)
	for _, want := range []string{
		`ambiguous bare identifier "tool"`,
		"runtime input has a field",
		"pipeline `as tool` binding",
		"was not evaluated",
	} {
		ktest.RequireCondition(t, strings.Contains(err.Error(), want), "runtime diagnostic %q does not contain %q", err, want)
	}
	for _, secret := range []string{"payload-secret", "binding-secret"} {
		ktest.RequireCondition(t, !strings.Contains(err.Error(), secret), "runtime diagnostic leaked %q: %v", secret, err)
	}
}

func TestComposedProjectionRuntimeConflictPreflightRunsBeforeEmbeddedActions(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	_, err = runner.Execute(t.Context(), cfg,
		`noop as tool -> { candidate: race { "ready" }, result: (tool.task) }`,
		"projection-composed-shadowing-runtime.nflow",
		map[string]any{"tool": map[string]any{"task": "payload-secret"}, "task": "binding-secret"},
	)
	ktest.RequireCondition(t, xerr.KindFrom(err) == xerr.KindConflict, "want Conflict before embedded projection evaluation, got %v", err)
	for _, secret := range []string{"payload-secret", "binding-secret"} {
		ktest.RequireCondition(t, !strings.Contains(err.Error(), secret), "runtime diagnostic leaked %q: %v", secret, err)
	}
}

func TestProjectionLocalLets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		assert []string
	}{
		{
			name:   "single binding used by sibling fields",
			source: `{ user: { profile: { name: "Ada", handle: "ada" } } } -> { let profile = .user.profile; first: profile.name, display: profile.name }`,
			assert: []string{`result.first == "Ada"`, `result.display == "Ada"`},
		},
		{
			name:   "multiple ordered bindings",
			source: `{ user: { profile: { name: "Ada", handle: "ada" } } } -> { let profile = .user.profile; let display = profile.name; first: display, second: profile.handle }`,
			assert: []string{`result.first == "Ada"`, `result.second == "ada"`},
		},
		{
			name:   "bindings coexist with shorthand and spread",
			source: `{ name: "Ada", status: "new", profile: { name: "A" } } -> { let profile = .profile; ..., name, display: profile.name, status: "active" }`,
			assert: []string{`result.name == "Ada"`, `result.display == "A"`, `result.status == "active"`},
		},
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			execution, execErr := runner.Execute(t.Context(), cfg, tc.source, "projection-let.nflow", nil)
			ktest.RequireNoError(t, execErr)
			ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, tc.assert)), 0)
		})
	}
}

func TestProjectionLocalLetInvalidDeclarations(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	for _, source := range []string{
		`{ let profile = ; display: profile.name }`,
		`{ let profile = .user.profile; let profile = .user; display: profile.name }`,
		`{ let display = profile.name; let profile = .user.profile; display: display }`,
	} {
		_, compileErr := runner.Compile(t.Context(), cfg, source, "invalid-projection-let.nflow")
		ktest.RequireCondition(t, compileErr != nil, "invalid local let compiled: %s", source)
	}
}

func TestProjectionLocalLetDoesNotEscapeItsProjection(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	source := `{ user: { profile: { name: "Ada" } } } -> { let profile = .user.profile; display: profile.name } -> { escaped: profile.name }`
	result, err := runner.Execute(t.Context(), cfg, source, "projection-let-scope.nflow", nil)
	ktest.RequireNoError(t, err)
	output, ok := result.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "projection output = %#v, want object", result.Output)
	ktest.RequireEqual(t, output["escaped"], "profile.name")
}
