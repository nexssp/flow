package pipeline_test

import (
	"context"
	"slices"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/runner"
)

func TestPipelineModifiers_AppliedToMountedAction(t *testing.T) {
	src := `
@pipeline greeted:tag="api,public" :status=201
  runtime.const @{ value: "hello" }
@end

pipeline.greeted
`
	bundles := []core.Bundle{
		runtime.Bundle(nil),
		pipeline.Bundle(nil),
		modifiers_meta.Bundle(nil),
	}

	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)

	ex, err := runner.Execute(context.Background(), cfg, src, "test_pipeline.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "hello")

	act, found := ex.Resolver.Action("pipeline.greeted")
	ktest.RequireCondition(t, found, "pipeline.greeted was not mounted")

	meta := act.Describe()
	ktest.RequireCondition(t, meta != nil, "pipeline.greeted has no metadata")
	ktest.RequireEqual(t, meta.SuccessStatus, 201)
	ktest.RequireCondition(t, slices.Contains(meta.Tags, "api"),
		"expected 'api' tag on materialized action, got: %v", meta.Tags)
}

func TestPipelineWithoutModifiers_BackwardsCompatible(t *testing.T) {
	src := `
@pipeline plain
  runtime.const @{ value: "no-mods" }
@end

pipeline.plain
`
	bundles := []core.Bundle{
		runtime.Bundle(nil),
		pipeline.Bundle(nil),
	}

	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)

	ex, err := runner.Execute(context.Background(), cfg, src, "test_plain.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "no-mods")
}
