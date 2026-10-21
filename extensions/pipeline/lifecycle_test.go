package pipeline_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestMaterializePhaseCompilesAfterExternalDeclarations(t *testing.T) {
	t.Parallel()
	for _, id := range []string{pipeline.ID, "renamed-composition"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			composition := pipeline.Bundle(nil)
			composition.ID = id
			// Put the consumer first, before both native and external declarations.
			bundles := []compiler.Bundle{composition}
			for _, bundle := range native.Bundles() {
				if bundle.ID != pipeline.ID {
					bundles = append(bundles, bundle)
				}
			}
			bundles = append(bundles, compiler.Bundle{
				ID: "external-declarations",
				Materialize: func(req compiler.MaterializeReq) error {
					value := action.New("external.value", func(_ context.Context, _ any) (string, error) {
						return "declared before composition", nil
					}).Build()
					return req.Resolver.Mount(action.Library{Name: "external", Actions: []action.AnyAction{value}})
				},
			})
			cfg, err := runner.BuildConfig(bundles)
			ktest.RequireNoError(t, err)
			result, err := runner.Execute(t.Context(), cfg, `@pipeline composed
  external.value
@end
pipeline.composed
`, "lifecycle-order.nflow", nil)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, result.Output, any("declared before composition"))
		})
	}
}
