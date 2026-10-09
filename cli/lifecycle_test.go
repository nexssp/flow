package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
)

func writeLifecycleFlow(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lifecycle.nflow")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func lifecycleBundle(id string, run func(context.Context) (any, error), shutdown func(context.Context) error) core.Bundle {
	act := action.New(id+".run", func(ctx context.Context, _ any) (any, error) {
		if run == nil {
			return "ok", nil
		}
		return run(ctx)
	}).Build()
	return core.Bundle{
		ID:        id,
		Libraries: []action.Library{{Name: id, Actions: []action.AnyAction{act}}},
		Shutdowns: []core.ShutdownFunc{shutdown},
	}
}

func TestRunWithBundles_ClosesOnHelpAndVersionEarlyReturns(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}, nil, {"not-a-command"}, {"list", "--not-a-flag"}} {
		calls := atomic.Int32{}
		bundle := core.Bundle{ID: "lifecycle_early", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			calls.Add(1)
			return nil
		}}}
		_, _ = captureIO(t, func() {
			_ = RunWithBundles(args, []core.Bundle{bundle})
		})
		if got := calls.Load(); got != 1 {
			t.Fatalf("RunWithBundles(%v) shutdown calls = %d, want 1", args, got)
		}
	}
}

func TestRunWithBundleFactories_ClosesEarlierBundleWhenLaterFactoryPanics(t *testing.T) {
	var closed atomic.Int32
	panicValue := &struct{ name string }{"later generated factory panic"}
	defer func() {
		if got := recover(); got != panicValue {
			t.Fatalf("factory panic changed: got %#v want %#v", got, panicValue)
		}
		if got := closed.Load(); got != 1 {
			t.Fatalf("earlier bundle cleanup calls = %d, want 1", got)
		}
	}()
	_ = RunWithBundleFactoriesForRequirements(nil, nil, func(adopt func(core.Bundle) error) error {
		bundle := core.Bundle{ID: "lifecycle_generated_first", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			closed.Add(1)
			return nil
		}}}
		if err := adopt(bundle); err != nil {
			return err
		}
		panic(panicValue)
	})
}

func TestRunWithBundlesForRequirements_UsesInjectedBundleForInfoAndCloses(t *testing.T) {
	t.Setenv(harnessEnv, "")
	var closed atomic.Int32
	bundle := core.Bundle{ID: "lifecycle_injected", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
		closed.Add(1)
		return nil
	}}}
	path := writeLifecycleFlow(t, "@require lifecycle.example\n@description \"info path\"\nruntime.const @{ value: \"ready\" }\n")

	code := RunWithBundlesForRequirements(
		[]string{"run", path, "--info"},
		[]core.Bundle{bundle},
		[]string{"lifecycle.example"},
	)
	if code != 0 {
		t.Fatalf("RunWithBundlesForRequirements info code = %d, want 0", code)
	}
	if got := closed.Load(); got != 1 {
		t.Fatalf("shutdown calls = %d, want 1", got)
	}
}

func TestRunEmbeddedWithBundles_InfoBranchesCloseOnSuccessAndParseError(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		wantOK bool
	}{
		{name: "success", source: "@description \"ok\"\nruntime.const @{ value: \"x\" }", wantOK: true},
		{name: "parse error", source: "@description \"broken\"\nruntime.const @{ value: }"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var closed atomic.Int32
			bundle := core.Bundle{ID: "lifecycle_embedded_info", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
				closed.Add(1)
				return nil
			}}}
			code := RunEmbeddedWithBundles(context.Background(), test.source, []string{"info"}, []core.Bundle{bundle})
			if test.wantOK && code != 0 {
				t.Fatalf("info code = %d, want 0", code)
			}
			if !test.wantOK && code == 0 {
				t.Fatal("expected info parse error")
			}
			if got := closed.Load(); got != 1 {
				t.Fatalf("shutdown calls = %d, want 1", got)
			}
		})
	}
}

func TestRunEmbeddedWithBundles_MaterializedPipelineDoesNotCloseBeforeAction(t *testing.T) {
	var closed atomic.Bool
	var ran atomic.Bool
	bundle := lifecycleBundle("lifecycle_pipeline", func(context.Context) (any, error) {
		if closed.Load() {
			return nil, errors.New("bundle was closed before nested action")
		}
		ran.Store(true)
		return "done", nil
	}, func(context.Context) error {
		closed.Store(true)
		return nil
	})
	source := "@pipeline nested\n  lifecycle_pipeline.run\n@end\npipeline.nested\n"
	code := RunEmbeddedWithBundles(context.Background(), source, []string{"--json"}, []core.Bundle{bundle})
	if code != 0 {
		t.Fatalf("embedded pipeline exit code = %d, want 0", code)
	}
	if !ran.Load() || !closed.Load() {
		t.Fatalf("action ran=%v shutdown=%v; want both true", ran.Load(), closed.Load())
	}
}

func TestRunEmbeddedWithBundleFactories_AdoptsBeforeInfoDispatch(t *testing.T) {
	var closed atomic.Int32
	code := RunEmbeddedWithBundleFactoriesForRequirements(
		context.Background(),
		"@description \"factory embedded\"\nruntime.const @{ value: 1 }",
		[]string{"info"},
		nil,
		func(adopt func(core.Bundle) error) error {
			return adopt(core.Bundle{ID: "lifecycle_factory_embedded", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
				closed.Add(1)
				return nil
			}}})
		},
	)
	if code != 0 || closed.Load() != 1 {
		t.Fatalf("embedded factory info code=%d cleanup=%d; want 0,1", code, closed.Load())
	}
}

func TestRunEmbeddedWithBundles_SupervisorChildrenFinishBeforeShutdown(t *testing.T) {
	var closed atomic.Bool
	var ran atomic.Bool
	bundle := lifecycleBundle("lifecycle_supervisor_child", func(context.Context) (any, error) {
		if closed.Load() {
			return nil, errors.New("supervisor child observed closed bundle")
		}
		ran.Store(true)
		return "done", nil
	}, func(context.Context) error {
		closed.Store(true)
		return nil
	})
	source := `supervisor.run @{ tasks: [{ id: "child", dsl: "lifecycle_supervisor_child.run", payload: {} }] }`
	code := RunEmbeddedWithBundles(context.Background(), source, []string{"--json"}, []core.Bundle{bundle})
	if code != 0 || !ran.Load() || !closed.Load() {
		t.Fatalf("supervisor exit=%d child-ran=%v shutdown=%v", code, ran.Load(), closed.Load())
	}
}

func TestRunEmbeddedWithBundles_CompileAndActionErrorsStillClose(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		run    func(context.Context) (any, error)
	}{
		{name: "compile", source: "runtime.const @{ value: }"},
		{name: "action", source: "lifecycle_error.run", run: func(context.Context) (any, error) {
			return nil, errors.New("primary action failure")
		}},
		{name: "action panic recovery", source: "lifecycle_error.run", run: func(context.Context) (any, error) {
			panic("action panic handled by Kernel")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var closed atomic.Int32
			bundle := lifecycleBundle("lifecycle_error", test.run, func(context.Context) error {
				closed.Add(1)
				return nil
			})
			code := RunEmbeddedWithBundles(context.Background(), test.source, nil, []core.Bundle{bundle})
			if code == 0 {
				t.Fatal("expected a nonzero result")
			}
			if got := closed.Load(); got != 1 {
				t.Fatalf("shutdown calls = %d, want 1", got)
			}
		})
	}
}

func TestRunEmbeddedWithBundles_CanceledActionReceivesFreshCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var cleanupErr error
	bundle := lifecycleBundle("lifecycle_cancel", func(ctx context.Context) (any, error) {
		return nil, ctx.Err()
	}, func(ctx context.Context) error {
		cleanupErr = ctx.Err()
		return cleanupErr
	})
	code := RunEmbeddedWithBundles(ctx, "lifecycle_cancel.run", nil, []core.Bundle{bundle})
	if code == 0 {
		t.Fatal("expected canceled action to fail")
	}
	if cleanupErr != nil {
		t.Fatalf("cleanup context was canceled: %v", cleanupErr)
	}
}

func TestRunEmbeddedWithBundles_CleanupErrorChangesOnlySuccessfulStatus(t *testing.T) {
	cleanupErr := errors.New("cleanup-only failure")
	bundle := core.Bundle{ID: "lifecycle_cleanup_error", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
		return cleanupErr
	}}}
	if code := RunEmbeddedWithBundles(context.Background(), "@description \"help\"", []string{"help"}, []core.Bundle{bundle}); code == 0 {
		t.Fatal("cleanup failure should convert success to nonzero")
	}
}

func TestCLICompileAndInspectionCommandsCloseAfterTheirHelpersReturn(t *testing.T) {
	path := writeLifecycleFlow(t, "@description \"compile path\"\nruntime.const @{ value: \"ok\" }\n")
	commands := [][]string{
		{"lint", path},
		{"expand", path},
		{"explain", path},
		{"catalog"},
		{"list"},
		{"show", "runtime.const"},
		{"serve", path}, // not an @on flow, so serve validates and exits with an error
	}
	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var closed atomic.Int32
			bundle := core.Bundle{ID: "lifecycle_compile_only", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
				closed.Add(1)
				return nil
			}}}
			code := RunWithBundles(args, []core.Bundle{bundle})
			if got := closed.Load(); got != 1 {
				t.Fatalf("%v shutdown calls = %d, want 1 (exit code %d)", args, got, code)
			}
			if args[0] != "serve" && code != 0 {
				t.Fatalf("%v exit code = %d, want 0", args, code)
			}
			if args[0] == "serve" && code == 0 {
				t.Fatal("serve without an @on handler should fail validation")
			}
		})
	}
}

func TestInvocationReusesNativeBundleInstancesForDirectivesAndConfig(t *testing.T) {
	var factories atomic.Int32
	var closed atomic.Int32
	var directiveCalls atomic.Int32
	inv, err := newInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	inv.nativeFactory = func() []core.Bundle {
		factories.Add(1)
		bundles := native.Bundles()
		return append(bundles, core.Bundle{
			ID: "lifecycle_native",
			Directives: []core.Directive{{
				Name: "lifecycle_probe",
				Handler: func(context.Context, core.DirectiveReq) (core.DirectiveRes, error) {
					directiveCalls.Add(1)
					return core.DirectiveRes{}, nil
				},
			}},
			Libraries: []action.Library{{Name: "lifecycle_native"}},
			Shutdowns: []core.ShutdownFunc{func(context.Context) error {
				closed.Add(1)
				return nil
			}},
		})
	}

	code := runInvocation(context.Background(), inv, func(ctx context.Context) int {
		directives, err := inv.directiveTable()
		if err != nil {
			t.Errorf("directiveTable: %v", err)
			return 1
		}
		if _, _, err := core.Preprocess(ctx, directives, "@lifecycle_probe\nruntime.const @{ value: 1 }", "same.nflow"); err != nil {
			t.Errorf("preprocess: %v", err)
			return 1
		}
		cfg, err := inv.buildConfig(nil)
		if err != nil {
			t.Errorf("buildConfig: %v", err)
			return 1
		}
		if _, ok := cfg.Directives.ByName("lifecycle_probe"); !ok {
			t.Error("config did not receive the directive from the cached native set")
			return 1
		}
		if closed.Load() != 0 {
			t.Error("native bundle closed before dispatch returned")
			return 1
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("invocation exit code = %d", code)
	}
	if factories.Load() != 1 || directiveCalls.Load() != 1 || closed.Load() != 1 {
		t.Fatalf("factories=%d directives=%d closes=%d; want 1 each", factories.Load(), directiveCalls.Load(), closed.Load())
	}
}

func TestInvocationAdoptsDynamicBundleBeforeOptionValidationFailure(t *testing.T) {
	var got []string
	core.Register("lifecycle_dynamic_first", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "lifecycle_dynamic_first", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			got = append(got, "first")
			return nil
		}}}
	})
	core.Register("lifecycle_dynamic_second", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "lifecycle_dynamic_second", AcceptedOptions: []string{}, Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			got = append(got, "second")
			return nil
		}}}
	})
	inv, err := newInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	code := runInvocation(context.Background(), inv, func(context.Context) int {
		_, err := inv.buildConfig([]require.Requirement{
			{Import: "lifecycle_dynamic_first"},
			{Import: "lifecycle_dynamic_second", Options: map[string]string{"bad": "value"}},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown option") {
			t.Errorf("buildConfig error = %v; expected unknown option", err)
		}
		return 1
	})
	if code == 0 {
		t.Fatal("expected config failure")
	}
	if gotText := strings.Join(got, ","); gotText != "second,first" {
		t.Fatalf("cleanup order = %s, want second,first", gotText)
	}
}

func TestInvocationDynamicFactoryPanicClosesPreviouslyReturnedBundles(t *testing.T) {
	var closed atomic.Int32
	var factoryRolledBack atomic.Bool
	panicValue := &struct{ name string }{"factory panic"}
	core.Register("lifecycle_panic_first", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "lifecycle_panic_first", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			closed.Add(1)
			return nil
		}}}
	})
	core.Register("lifecycle_panic_later", func(map[string]string) core.Bundle {
		partialResourceOpen := true
		defer func() {
			if recovered := recover(); recovered != nil {
				partialResourceOpen = false
				factoryRolledBack.Store(!partialResourceOpen)
				panic(recovered)
			}
		}()
		panic(panicValue)
	})
	inv, err := newInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if got := recover(); got != panicValue {
			t.Fatalf("factory panic changed: got %#v want %#v", got, panicValue)
		}
		if got := closed.Load(); got != 1 {
			t.Fatalf("earlier factory cleanup calls = %d, want 1", got)
		}
		if !factoryRolledBack.Load() {
			t.Fatal("panicking factory did not roll back its own partial resource")
		}
	}()
	_ = runInvocation(context.Background(), inv, func(context.Context) int {
		_, _ = inv.buildConfig([]require.Requirement{
			{Import: "lifecycle_panic_first"},
			{Import: "lifecycle_panic_later"},
		})
		return 0
	})
}

func TestBatchLintRetainsAndClosesBundlesAfterLaterConfigFailure(t *testing.T) {
	var closed []string
	core.Register("lifecycle_batch_good", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "lifecycle_batch_good", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			closed = append(closed, "good")
			return nil
		}}}
	})
	core.Register("lifecycle_batch_bad", func(map[string]string) core.Bundle {
		return core.Bundle{ID: "lifecycle_batch_bad", AcceptedOptions: []string{}, Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			closed = append(closed, "bad")
			return nil
		}}}
	})
	first := writeLifecycleFlow(t, "@require lifecycle_batch_good\nruntime.const @{ value: \"ok\" }\n")
	second := filepath.Join(t.TempDir(), "bad.nflow")
	if err := os.WriteFile(second, []byte("@require lifecycle_batch_bad { bad: \"value\" }\nruntime.const @{ value: \"ok\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := runLint([]string{first, second})
	if code == 0 {
		t.Fatal("expected batch lint config failure")
	}
	if got := strings.Join(closed, ","); got != "bad,good" {
		t.Fatalf("batch cleanup order = %s, want bad,good", got)
	}
}

func TestRunPathInfoRemainsCompatible(t *testing.T) {
	path := writeLifecycleFlow(t, "@description \"path entry\"\nruntime.const @{ value: \"ok\" }\n")
	if code := RunPath(context.Background(), []string{path, "--info"}); code != 0 {
		t.Fatalf("RunPath info exit code = %d", code)
	}
}
