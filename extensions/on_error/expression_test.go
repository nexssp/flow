package on_error_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
	onerr "github.com/nexssp/flow/extensions/on_error"
	flowretry "github.com/nexssp/flow/extensions/retry"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func newGuardConfig(t *testing.T, primaryCause, handlerCause error, recoverCalls, fallbackCalls *atomic.Int64) runner.Config {
	t.Helper()

	actions := []action.AnyAction{
		action.New("on_error_test.fail", func(_ context.Context, input any) (any, error) {
			kind := xerr.KindInternal
			if m, ok := input.(map[string]any); ok {
				if raw, ok := m["kind"].(string); ok && raw != "" {
					kind = xerr.Kind(raw)
				}
			}
			return nil, &xerr.AppError{Kind: kind, Message: "synthetic failure", Cause: primaryCause}
		}).Build(),
		action.New("on_error_test.fail_sentinel", func(context.Context, any) (any, error) {
			return nil, primaryCause
		}).Build(),
		action.New("on_error_test.fail_deadline", func(context.Context, any) (any, error) {
			return nil, context.DeadlineExceeded
		}).Build(),
		action.New("on_error_test.fail_cancel", func(context.Context, any) (any, error) {
			return nil, context.Canceled
		}).Build(),
		action.New("on_error_test.retry_success", func(_ context.Context, input any) (any, error) {
			m, _ := input.(map[string]any)
			attempts, _ := m["attempts"].(*atomic.Int64)
			if attempts == nil {
				return nil, xerr.BadRequest("test retry action requires an attempt counter")
			}
			if attempts.Add(1) <= 2 {
				return nil, xerr.Unavailable("temporary retry probe")
			}
			return "retry-success", nil
		}).Build(),
		action.New("on_error_test.retry_unavailable", func(_ context.Context, input any) (any, error) {
			m, _ := input.(map[string]any)
			attempts, _ := m["attempts"].(*atomic.Int64)
			if attempts == nil {
				return nil, xerr.BadRequest("test retry action requires an attempt counter")
			}
			attempts.Add(1)
			return nil, xerr.Unavailable("retry probe exhausted")
		}).Build(),
		action.New("on_error_test.recover_kind", func(_ context.Context, input any) (any, error) {
			recoverCalls.Add(1)
			m, _ := input.(map[string]any)
			errorInfo, _ := m["error"].(map[string]any)
			return errorInfo["kind"], nil
		}).Build(),
		action.New("on_error_test.inspect", func(_ context.Context, input any) (any, error) {
			recoverCalls.Add(1)
			m, _ := input.(map[string]any)
			return map[string]any{
				"tag":   m["tag"],
				"input": m["input"],
				"error": m["error"],
			}, nil
		}).Build(),
		action.New("on_error_test.recover_fail", func(context.Context, any) (any, error) {
			recoverCalls.Add(1)
			return "partial-handler-result", handlerCause
		}).Build(),
		action.New("on_error_test.downstream", func(_ context.Context, input any) (any, error) {
			m, _ := input.(map[string]any)
			out := make(map[string]any, len(m)+1)
			maps.Copy(out, m)
			out["continued"] = true
			return out, nil
		}).Build(),
		action.New("on_error_test.fallback", func(context.Context, any) (any, error) {
			fallbackCalls.Add(1)
			return "fallback", nil
		}).Build(),
		action.New("on_error_test.pipeline_recover", func(context.Context, any) (any, error) {
			recoverCalls.Add(1)
			return "pipeline-recovered", nil
		}).Build(),
	}

	bundles := native.Bundles()
	for i := range bundles {
		if bundles[i].ID == flowretry.ID {
			bundles[i] = flowretry.BundleWithConfig(flowretry.Config{
				MinBackoff: time.Nanosecond,
				MaxBackoff: time.Nanosecond,
			})
		}
	}
	bundles = append(bundles, core.Bundle{
		ID: "on-error-expression-tests",
		Libraries: []action.Library{{
			Name:    "on_error_test",
			Actions: actions,
		}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	return cfg
}

func TestQuestionMarkTernariesRemainCompatibleWithOnErrorInstalled(t *testing.T) {
	cfg := newGuardConfig(t, errors.New("primary"), errors.New("handler"), new(atomic.Int64), new(atomic.Int64))
	for _, source := range []string{
		`a ? b`,
		`a ? b : c`,
		`a ? on_error : c`,
	} {
		t.Run(source, func(t *testing.T) {
			parsed, err := core.NewParserWithPrimaries(context.Background(), cfg.Operators, cfg.Primaries, source).Parse()
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if _, ok := parsed.(*core.ConditionalExpr); !ok {
				t.Fatalf("parsed expression = %T, want *core.ConditionalExpr", parsed)
			}
		})
	}
}

func TestErrorGuardAllBuiltinKindsResolve(t *testing.T) {
	primaryCause := errors.New("primary-cause")
	for _, kind := range xerr.AllKinds() {
		t.Run(string(kind), func(t *testing.T) {
			recoverCalls := new(atomic.Int64)
			cfg := newGuardConfig(t, primaryCause, errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
			source := fmt.Sprintf("on_error_test.fail ? on_error { %s -> on_error_test.recover_kind, else -> on_error_test.fallback }", "xerr.Kind"+string(kind))
			result, err := runner.Execute(context.Background(), cfg, source, "kind-symbol.nflow", map[string]any{"kind": string(kind)})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if result.Output != kind {
				t.Fatalf("output = %#v, want typed kind %q", result.Output, kind)
			}
			if got := recoverCalls.Load(); got != 1 {
				t.Fatalf("recovery calls = %d, want 1", got)
			}
		})
	}
}

func TestErrorGuardElseHandlesUnknownCustomKind(t *testing.T) {
	primaryCause := errors.New("primary-cause")
	recoverCalls := new(atomic.Int64)
	cfg := newGuardConfig(t, primaryCause, errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
	source := `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.fallback, else -> on_error_test.recover_kind }`
	result, err := runner.Execute(context.Background(), cfg, source, "custom-kind.nflow", map[string]any{"kind": "VendorSpecific"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output != xerr.Kind("VendorSpecific") {
		t.Fatalf("output = %#v, want custom kind", result.Output)
	}
	if got := recoverCalls.Load(); got != 1 {
		t.Fatalf("recovery calls = %d, want 1", got)
	}
}

func TestErrorGuardExposesLocalInputAndErrorMetadataThenContinues(t *testing.T) {
	primaryCause := errors.New("primary-cause")
	recoverCalls := new(atomic.Int64)
	cfg := newGuardConfig(t, primaryCause, errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
	source := `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.inspect } -> on_error_test.downstream`
	input := map[string]any{"kind": "Timeout", "tag": "original"}
	result, err := runner.Execute(context.Background(), cfg, source, "scope.nflow", input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T, want map", result.Output)
	}
	if output["continued"] != true {
		t.Fatalf("downstream continuation missing: %#v", output)
	}
	seenInput, ok := output["input"].(map[string]any)
	if !ok || seenInput["tag"] != "original" {
		t.Fatalf("recovery input = %#v, want original payload", output["input"])
	}
	if output["tag"] != "original" {
		t.Fatalf("local payload field = %#v, want original", output["tag"])
	}
	errorInfo, ok := output["error"].(map[string]any)
	if !ok {
		t.Fatalf("error metadata = %T, want map", output["error"])
	}
	cause, ok := errorInfo["cause"].(error)
	if errorInfo["kind"] != xerr.KindTimeout || !ok || !errors.Is(cause, primaryCause) {
		t.Fatalf("error metadata = %#v, want typed Timeout and original cause", errorInfo)
	}
	original, ok := errorInfo["original"].(error)
	var originalAppError *xerr.AppError
	if !ok || !errors.As(original, &originalAppError) {
		t.Fatalf("error.original = %T, want an error wrapping *xerr.AppError", errorInfo["original"])
	}
	if _, leaked := input["error"]; leaked {
		t.Fatalf("recovery scope leaked into original input: %#v", input)
	}
	if _, leaked := input["input"]; leaked {
		t.Fatalf("recovery scope leaked input binding into original input: %#v", input)
	}
}

func TestErrorGuardNoMatchReturnsOriginalErrorUnchanged(t *testing.T) {
	primary := errors.New("sentinel-primary")
	cfg := newGuardConfig(t, primary, errors.New("handler-cause"), new(atomic.Int64), new(atomic.Int64))
	source := `on_error_test.fail_sentinel ? on_error { xerr.KindTimeout -> on_error_test.recover_kind }`
	result, err := runner.Execute(context.Background(), cfg, source, "no-match.nflow", map[string]any{})
	if !errors.Is(err, primary) {
		t.Fatalf("error = %v (%T), want original error cause %v", err, err, primary)
	}
	if result.Output != nil {
		t.Fatalf("partial output = %#v, want nil", result.Output)
	}
}

func TestErrorGuardHandlerFailurePreservesBothErrorsAndDiscardsPartialOutput(t *testing.T) {
	primary := errors.New("sentinel-primary")
	handler := errors.New("sentinel-handler")
	cfg := newGuardConfig(t, primary, handler, new(atomic.Int64), new(atomic.Int64))
	source := `on_error_test.fail_sentinel ? on_error { else -> on_error_test.recover_fail }`
	result, err := runner.Execute(context.Background(), cfg, source, "handler-error.nflow", map[string]any{})
	if !errors.Is(err, primary) || !errors.Is(err, handler) {
		t.Fatalf("error = %v; want both primary and handler causes", err)
	}
	if result.Output != nil {
		t.Fatalf("partial output = %#v, want nil", result.Output)
	}
}

func TestErrorGuardNestedAndInteractsWithFallbackAndPipelineRouting(t *testing.T) {
	primary := errors.New("primary-cause")
	recoverCalls := new(atomic.Int64)
	fallbackCalls := new(atomic.Int64)
	cfg := newGuardConfig(t, primary, errors.New("handler-cause"), recoverCalls, fallbackCalls)

	t.Run("nested guards", func(t *testing.T) {
		source := `on_error_test.fail ? on_error { xerr.KindInternal -> on_error_test.fail ? on_error { xerr.KindInternal -> on_error_test.recover_kind } }`
		result, err := runner.Execute(context.Background(), cfg, source, "nested.nflow", map[string]any{"kind": "Internal"})
		if err != nil || result.Output != xerr.KindInternal {
			t.Fatalf("Execute = (%#v, %v), want Internal", result.Output, err)
		}
	})

	t.Run("recovered left side prevents || fallback", func(t *testing.T) {
		source := `on_error_test.fail ? on_error { xerr.KindInternal -> on_error_test.recover_kind } || on_error_test.fallback`
		result, err := runner.Execute(context.Background(), cfg, source, "guard-or.nflow", map[string]any{"kind": "Internal"})
		if err != nil || result.Output != xerr.KindInternal {
			t.Fatalf("Execute = (%#v, %v), want Internal", result.Output, err)
		}
		if got := fallbackCalls.Load(); got != 0 {
			t.Fatalf("fallback calls = %d, want 0", got)
		}
	})

	t.Run("unmatched guard error reaches || fallback", func(t *testing.T) {
		source := `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.recover_kind } || on_error_test.fallback`
		result, err := runner.Execute(context.Background(), cfg, source, "guard-or-fallback.nflow", map[string]any{"kind": "Internal"})
		if err != nil || result.Output != "fallback" {
			t.Fatalf("Execute = (%#v, %v), want fallback", result.Output, err)
		}
		if got := fallbackCalls.Load(); got != 1 {
			t.Fatalf("fallback calls = %d, want 1", got)
		}
	})

	t.Run("outer @on_error remains pipeline-level", func(t *testing.T) {
		source := "@on_error {\n  when error.kind == \"Internal\" -> on_error_test.pipeline_recover\n}\non_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.recover_kind }"
		result, err := runner.Execute(context.Background(), cfg, source, "pipeline-route.nflow", map[string]any{"kind": "Internal"})
		if err != nil || result.Output != "pipeline-recovered" {
			t.Fatalf("Execute = (%#v, %v), want pipeline-recovered", result.Output, err)
		}
	})

	t.Run("outer @on_error accepts symbolic Kernel kinds", func(t *testing.T) {
		source := "@on_error {\n  when error.kind == xerr.KindInternal -> on_error_test.pipeline_recover\n}\non_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.recover_kind }"
		result, err := runner.Execute(context.Background(), cfg, source, "pipeline-symbol.nflow", map[string]any{"kind": "Internal"})
		if err != nil || result.Output != "pipeline-recovered" {
			t.Fatalf("Execute = (%#v, %v), want pipeline-recovered", result.Output, err)
		}
	})

	t.Run("nested match sees typed kind symbols", func(t *testing.T) {
		source := `on_error_test.fail ? on_error { xerr.KindInternal -> match(.error.kind) { xerr.KindInternal -> on_error_test.recover_kind, default -> on_error_test.fallback } }`
		result, err := runner.Execute(context.Background(), cfg, source, "nested-match.nflow", map[string]any{"kind": "Internal"})
		if err != nil || result.Output != xerr.KindInternal {
			t.Fatalf("Execute = (%#v, %v), want typed Internal", result.Output, err)
		}
	})
}

func TestErrorGuardCallerAndSignalContextCancellationNeverInvokeRecovery(t *testing.T) {
	primary := errors.New("primary-cause")
	for _, name := range []string{"caller-cancel", "signal-shutdown"} {
		t.Run(name, func(t *testing.T) {
			recoverCalls := new(atomic.Int64)
			fallbackCalls := new(atomic.Int64)
			cfg := newGuardConfig(t, primary, errors.New("handler-cause"), recoverCalls, fallbackCalls)
			var ctx context.Context
			var cleanup func()
			if name == "signal-shutdown" {
				// A signal.NotifyContext shutdown has this same canceled-context
				// contract and can carry a signal cause.
				var cancel context.CancelCauseFunc
				ctx, cancel = context.WithCancelCause(context.Background())
				cancel(errors.New("signal: SIGTERM"))
				cleanup = func() { cancel(nil) }
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
				cleanup = cancel
			}
			defer cleanup()
			source := `on_error_test.fail ? on_error { else -> on_error_test.recover_kind } || on_error_test.fallback`
			_, err := runner.Execute(ctx, cfg, source, "cancel.nflow", map[string]any{})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if got := recoverCalls.Load(); got != 0 {
				t.Fatalf("recovery calls = %d, want 0", got)
			}
			if got := fallbackCalls.Load(); got != 0 {
				t.Fatalf("fallback calls = %d, want 0", got)
			}
		})
	}
}

func TestErrorGuardSyntaxAndCompileErrors(t *testing.T) {
	cfg := newGuardConfig(t, errors.New("primary"), errors.New("handler"), new(atomic.Int64), new(atomic.Int64))
	cases := []struct {
		name   string
		source string
	}{
		{"unknown kind symbol", `on_error_test.fail ? on_error { xerr.KindVendor -> on_error_test.recover_kind }`},
		{"quoted kind is not a typed symbol", `on_error_test.fail ? on_error { "Timeout" -> on_error_test.recover_kind }`},
		{"missing arrow", `on_error_test.fail ? on_error { xerr.KindTimeout on_error_test.recover_kind }`},
		{"duplicate kind", `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.recover_kind, xerr.KindTimeout -> on_error_test.recover_kind }`},
		{"else must be last", `on_error_test.fail ? on_error { else -> on_error_test.recover_kind, xerr.KindTimeout -> on_error_test.recover_kind }`},
		{"missing closing brace", `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.recover_kind`},
		{"empty guard", `on_error_test.fail ? on_error {}`},
		{"unknown handler action", `on_error_test.fail ? on_error { else -> on_error_test.missing }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runner.Compile(context.Background(), cfg, tc.source, tc.name); err == nil {
				t.Fatalf("Compile(%q) succeeded, want error", tc.source)
			}
		})
	}
}

func TestErrorGuardStandardContextKindsAndTerminalCallerDeadline(t *testing.T) {
	primary := errors.New("primary-cause")
	recoverCalls := new(atomic.Int64)
	cfg := newGuardConfig(t, primary, errors.New("handler-cause"), recoverCalls, new(atomic.Int64))

	t.Run("inner standard deadline maps to Timeout and is recoverable", func(t *testing.T) {
		source := `on_error_test.fail_deadline ? on_error { xerr.KindTimeout -> on_error_test.recover_kind }`
		result, err := runner.Execute(context.Background(), cfg, source, "inner-deadline.nflow", map[string]any{})
		if err != nil || result.Output != xerr.KindTimeout {
			t.Fatalf("Execute = (%#v, %v), want recoverable xerr.KindTimeout", result.Output, err)
		}
		if got := recoverCalls.Load(); got != 1 {
			t.Fatalf("recovery calls = %d, want 1", got)
		}
	})

	t.Run("standard Canceled error remains terminal with live outer context", func(t *testing.T) {
		source := `on_error_test.fail_cancel ? on_error { xerr.KindCanceled -> on_error_test.recover_kind, else -> on_error_test.fallback }`
		_, err := runner.Execute(context.Background(), cfg, source, "inner-cancel.nflow", map[string]any{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if got := recoverCalls.Load(); got != 1 {
			t.Fatalf("recovery calls = %d, want no new recovery for cancellation", got)
		}
	})

	t.Run("expired caller deadline is terminal", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
		defer cancel()
		source := `on_error_test.fail ? on_error { else -> on_error_test.recover_kind }`
		_, err := runner.Execute(ctx, cfg, source, "outer-deadline.nflow", map[string]any{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want caller context deadline", err)
		}
		if got := recoverCalls.Load(); got != 1 {
			t.Fatalf("recovery calls = %d, want no new recovery for caller deadline", got)
		}
	})
}

func TestErrorGuardRecoversInnerTimeoutModifier(t *testing.T) {
	cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), new(atomic.Int64), new(atomic.Int64))
	source := `runtime.sleep:timeout=5ms @{ duration_ms: 80 } ? on_error { xerr.KindTimeout -> on_error_test.recover_kind }`
	result, err := runner.Execute(context.Background(), cfg, source, "modifier-timeout.nflow", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output != xerr.KindTimeout {
		t.Fatalf("output = %#v, want xerr.KindTimeout", result.Output)
	}
}

func TestErrorGuardRunsAfterRetrySucceedsOrExhausts(t *testing.T) {
	t.Run("retry success prevents recovery", func(t *testing.T) {
		attempts := new(atomic.Int64)
		recoverCalls := new(atomic.Int64)
		cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
		source := `on_error_test.retry_success:retry=2 ? on_error { xerr.KindUnavailable -> on_error_test.recover_kind }`
		result, err := runner.Execute(context.Background(), cfg, source, "retry-success.nflow", map[string]any{"attempts": attempts})
		if err != nil || result.Output != "retry-success" {
			t.Fatalf("Execute = (%#v, %v), want retry-success", result.Output, err)
		}
		if got := attempts.Load(); got != 3 {
			t.Fatalf("attempts = %d, want initial call plus two retries", got)
		}
		if got := recoverCalls.Load(); got != 0 {
			t.Fatalf("recovery calls = %d, want 0 after successful retry", got)
		}
	})

	t.Run("recovery runs once after retry exhaustion", func(t *testing.T) {
		attempts := new(atomic.Int64)
		recoverCalls := new(atomic.Int64)
		cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
		source := `on_error_test.retry_unavailable:retry=2 ? on_error { xerr.KindUnavailable -> on_error_test.recover_kind }`
		result, err := runner.Execute(context.Background(), cfg, source, "retry-exhausted.nflow", map[string]any{"attempts": attempts})
		if err != nil || result.Output != xerr.KindUnavailable {
			t.Fatalf("Execute = (%#v, %v), want recovered Unavailable", result.Output, err)
		}
		if got := attempts.Load(); got != 3 {
			t.Fatalf("attempts = %d, want initial call plus two retries", got)
		}
		if got := recoverCalls.Load(); got != 1 {
			t.Fatalf("recovery calls = %d, want 1 after retry exhaustion", got)
		}
	})
}

func TestPipelineOnErrorDoesNotSwallowSignalCancellation(t *testing.T) {
	recoverCalls := new(atomic.Int64)
	cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("signal: SIGTERM"))
	defer cancel(nil)
	source := "@on_error {\n  else -> on_error_test.pipeline_recover\n}\non_error_test.fail_sentinel"
	_, err := runner.Execute(ctx, cfg, source, "pipeline-signal-shutdown.nflow", map[string]any{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := recoverCalls.Load(); got != 0 {
		t.Fatalf("recovery calls = %d, want 0 during signal-driven shutdown", got)
	}
}

func TestErrorGuardLocalScopeIsolatedAcrossConcurrentRuns(t *testing.T) {
	cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), new(atomic.Int64), new(atomic.Int64))
	source := `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.inspect }`
	compiled, err := runner.Compile(context.Background(), cfg, source, "local-scope.nflow")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tag := fmt.Sprintf("request-%d", i)
			input := map[string]any{"kind": "Timeout", "tag": tag}
			outputValue, runErr := action.InvokeAny(context.Background(), compiled.Program, input)
			if runErr != nil {
				errs <- runErr
				return
			}
			output, ok := outputValue.(map[string]any)
			if !ok || output["tag"] != tag {
				errs <- fmt.Errorf("run %d output = %#v, want tag %q", i, outputValue, tag)
				return
			}
			seenInput, ok := output["input"].(map[string]any)
			if !ok || seenInput["tag"] != tag {
				errs <- fmt.Errorf("run %d input scope = %#v, want tag %q", i, output["input"], tag)
				return
			}
			if _, leaked := input["error"]; leaked {
				errs <- fmt.Errorf("run %d mutated original input: %#v", i, input)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestErrorGuardCustomKindWithoutElsePropagates(t *testing.T) {
	recoverCalls := new(atomic.Int64)
	cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), recoverCalls, new(atomic.Int64))
	source := `on_error_test.fail ? on_error { xerr.KindTimeout -> on_error_test.recover_kind }`
	result, err := runner.Execute(context.Background(), cfg, source, "custom-no-else.nflow", map[string]any{"kind": "VendorSpecific"})
	if err == nil || result.Output != nil {
		t.Fatalf("Execute = (%#v, %v), want propagated custom error and nil output", result.Output, err)
	}
	if got := xerr.From(err).Kind; got != xerr.Kind("VendorSpecific") {
		t.Fatalf("propagated kind = %q, want VendorSpecific", got)
	}
	if got := recoverCalls.Load(); got != 0 {
		t.Fatalf("recovery calls = %d, want 0", got)
	}
}

func TestExpressionGuardFixtureRuns(t *testing.T) {
	fixture, err := fs.ReadFile(onerr.Bundle(nil).Fixtures, "nflows/expression_guard.nflow")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cfg := newGuardConfig(t, errors.New("primary-cause"), errors.New("handler-cause"), new(atomic.Int64), new(atomic.Int64))
	result, err := runner.Execute(context.Background(), cfg, string(fixture), "expression_guard.nflow", map[string]any{})
	if err != nil {
		t.Fatalf("execute fixture: %v", err)
	}
	if result.Output != "deferred" {
		t.Fatalf("fixture output = %#v, want deferred", result.Output)
	}
}
