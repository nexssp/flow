package runner_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xfile"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/contracts"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type compiledTestFileResolver struct{}

func (compiledTestFileResolver) Open(context.Context, xfile.Ref) (io.ReadCloser, xfile.Info, error) {
	return nil, xfile.Info{}, errors.New("unexpected file open")
}

func TestExecuteCompiledReusesProgramAndExecutionContext(t *testing.T) {
	var materializations atomic.Int32
	inspect := action.New("test.inspect", func(ctx context.Context, input any) (any, error) {
		return map[string]any{
			"payload":         input,
			"compiler":        contracts.CompilerFromContext(ctx) != nil,
			"action_resolver": contracts.ActionResolverFromContext(ctx) != nil,
			"file_resolver":   contracts.FileResolverFromContext(ctx) != nil,
		}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "execute-compiled-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{inspect}}},
		Materialize: func(compiler.MaterializeReq) error {
			materializations.Add(1)
			return nil
		},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	cfg.FileResolver = compiledTestFileResolver{}
	source := "@schema Request {\n  Name string `json:\"name\" default:\"schema-name\" validate:\"required\"`\n}\n\n@pipeline handle:schema=Request\n  test.inspect\n@end\n\npipeline.handle\n"
	name := "execute-compiled.nflow"
	compiled, err := runner.Compile(t.Context(), cfg, source, name)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := materializations.Load(); got != 1 {
		t.Fatalf("materializations after compile = %d, want 1", got)
	}

	var executionIDs []string
	for i := range 2 {
		result, runErr := runner.ExecuteCompiled(t.Context(), cfg, compiled, source, name, map[string]any{})
		if runErr != nil {
			t.Fatalf("ExecuteCompiled call %d: %v", i+1, runErr)
		}
		if result.CompileDuration != 0 {
			t.Errorf("CompileDuration = %s, want zero for precompiled execution", result.CompileDuration)
		}
		if result.RunDuration < 0 {
			t.Errorf("RunDuration = %s, want non-negative", result.RunDuration)
		}
		if result.ExecutionID == "" {
			t.Errorf("call %d returned empty ExecutionID", i+1)
		}
		executionIDs = append(executionIDs, result.ExecutionID)

		outer, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("output type = %T, want map[string]any", result.Output)
		}
		for _, key := range []string{"compiler", "action_resolver", "file_resolver"} {
			if value, isBool := outer[key].(bool); !isBool || !value {
				t.Errorf("context field %q = %#v, want true", key, outer[key])
			}
		}
		payload, ok := outer["payload"].(map[string]any)
		if !ok {
			t.Fatalf("captured payload type = %T, want map[string]any", outer["payload"])
		}
		if got := payload["name"]; got != "schema-name" {
			t.Errorf("schema default name = %#v, want schema-name", got)
		}
	}
	if executionIDs[0] == executionIDs[1] {
		t.Errorf("repeated calls reused execution identity %q", executionIDs[0])
	}
	if got := materializations.Load(); got != 1 {
		t.Errorf("materializations after repeated runs = %d, want 1", got)
	}
}

func TestExecuteCompiledRejectsNilProgram(t *testing.T) {
	result, err := runner.ExecuteCompiled(t.Context(), runner.Config{}, runner.Compilation{}, "", "", nil)
	if err == nil || err.Error() != "compile: source is empty" {
		t.Fatalf("error = %v, want compile: source is empty", err)
	}
	if result.ExecutionID == "" {
		t.Fatal("nil-program result has no execution identity")
	}
	if result.CompileDuration != 0 {
		t.Errorf("CompileDuration = %s, want zero", result.CompileDuration)
	}
}

func TestExecuteCompiledPropagatesCancellation(t *testing.T) {
	waitForContext := action.New("test.wait_for_context", func(ctx context.Context, _ any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}).Build()
	bundle := compiler.Bundle{
		ID:        "execute-compiled-cancel-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{waitForContext}}},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	source := "test.wait_for_context"
	name := "execute-compiled-cancel.nflow"
	compiled, err := runner.Compile(t.Context(), cfg, source, name)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := runner.ExecuteCompiled(ctx, cfg, compiled, source, name, map[string]any{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.ExecutionID == "" {
		t.Fatal("canceled execution has no execution identity")
	}
}
