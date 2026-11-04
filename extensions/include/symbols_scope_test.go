package include

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/contracts"
	matchext "github.com/nexssp/flow/extensions/match"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	projectionext "github.com/nexssp/flow/extensions/projection"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	symbolsext "github.com/nexssp/flow/extensions/symbols"
	"github.com/nexssp/flow/runner"
)

func TestIncludedPolicyKeepsLocalSymbolScopeAcrossCompilationPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rootFile := filepath.Join(dir, "root.nflow")
	childFile := filepath.Join(dir, "policy.nflow")
	childSource := `@symbols route { active, inactive }
@symbols output_mode { clipboard, stdout, check, file }

@pipeline static_match
  {
    mode: match(.policy.mode) {
      route.active -> output_mode.clipboard,
      _ -> output_mode.stdout
    }
  }
@end

@pipeline dynamic_match
  test.compile_policy
@end
`
	ktest.RequireNoError(t, os.WriteFile(childFile, []byte(childSource), 0o600))

	dynamicPolicy := action.New("test.compile_policy", func(ctx context.Context, input any) (any, error) {
		pipelineCompiler := contracts.CompilerFromContext(ctx)
		if pipelineCompiler == nil {
			return nil, errors.New("pipeline compiler missing from action context")
		}
		program, err := pipelineCompiler.CompilePipeline(`match(.policy.mode) {
			route.active -> runtime.const @{ value: output_mode.clipboard },
			_ -> runtime.const @{ value: output_mode.stdout }
		}`)
		if err != nil {
			return nil, err
		}
		return program.ExecuteDecoded(ctx, func(target any) error {
			return action.Assign(target, input)
		})
	}).Build()

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		Bundle(nil),
		symbolsext.Bundle(nil),
		matchext.Bundle(nil),
		pipelineext.Bundle(nil),
		projectionext.Bundle(nil),
		runtimeext.Bundle(nil),
		{ID: "included_dynamic_policy", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{dynamicPolicy}}}},
	})
	ktest.RequireNoError(t, err)

	for _, tc := range []struct {
		name string
		want any
	}{
		{name: "static_match", want: map[string]any{"mode": "clipboard"}},
		{name: "dynamic_match", want: "clipboard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rootSource := "@include ./policy.nflow AS policy\npolicy.pipeline." + tc.name
			result, runErr := runner.Execute(t.Context(), cfg, rootSource, rootFile,
				map[string]any{"policy": map[string]any{"mode": "active"}},
			)
			ktest.RequireNoError(t, runErr)
			ktest.RequireEqual(t, result.Output, tc.want)
		})
	}
}
