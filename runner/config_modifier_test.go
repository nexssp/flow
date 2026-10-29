package runner_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestMaterializedPipelinesResolveConfigTimeoutsOnPrimaryActions(t *testing.T) {
	bundles := append(native.Bundles(), compiler.Bundle{
		ID:        "test_timeout_primary",
		Libraries: []action.Library{{Name: "test_timeout_primary"}},
		Primaries: []compiler.PrimaryExtension{testTimeoutPrimary{}},
	})
	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)

	source := `@config {
  chat_pipeline_timeout: "240s"
  memory_search_pipeline_timeout: "65s"
  memory_put_pipeline_timeout: "125s"
  memory_put_action_timeout: "120s"
}
@pipeline chat_with_memory :timeout=@config.chat_pipeline_timeout
  timeout.file
  -> pipeline.memory_search
@end
@pipeline memory_put :timeout=@config.memory_put_pipeline_timeout
  timeout.file :timeout=@config.memory_put_action_timeout
@end
@pipeline memory_search :timeout=@config.memory_search_pipeline_timeout
  timeout.file
@end
runtime.const @{ value: {} } -> pipeline.chat_with_memory`

	_, err = runner.Compile(t.Context(), cfg, source, "memory-timeouts.nflow")
	ktest.RequireNoError(t, err)
}

type testTimeoutPrimary struct{}

func (testTimeoutPrimary) Name() string    { return "test.timeout_primary" }
func (testTimeoutPrimary) Keyword() string { return "timeout.file" }
func (testTimeoutPrimary) Parse(p *compiler.Parser) (compiler.Expr, error) {
	p.Advance()
	return &testTimeoutExpr{}, nil
}

type testTimeoutExpr struct{}

func (*testTimeoutExpr) Node()                                     {}
func (*testTimeoutExpr) Children() []compiler.Expr                 { return nil }
func (*testTimeoutExpr) Analyze(compiler.CapabilityResolver) error { return nil }
func (*testTimeoutExpr) Build(context.Context, *compiler.BuildContext) (action.AnyAction, error) {
	return action.New("test.timeout_action", func(_ context.Context, input any) (any, error) {
		return input, nil
	}).Build(), nil
}
