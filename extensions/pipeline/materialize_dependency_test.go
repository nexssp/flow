package pipeline

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func TestMaterializePipelinesResolvesForwardLocalReferences(t *testing.T) {
	resolver, err := compiler.NewDynamicResolver()
	ktest.RequireNoError(t, err)

	var compileOrder []string
	req := compiler.MaterializeReq{
		Meta:     map[string]any{},
		Resolver: resolver,
		Compile: func(name, _ string, _ ...string) (action.AnyAction, error) {
			compileOrder = append(compileOrder, name)
			if name == "pipeline.local_chain" {
				if _, ok := resolver.Action("pipeline.local_support"); !ok {
					return nil, xerr.Validation(`distribute.map.action: unknown capability "pipeline.local_support"`)
				}
			}
			return action.New("pipeline.test_body", func(_ context.Context, input any) (any, error) {
				return input, nil
			}).Build(), nil
		},
	}

	// This order intentionally puts the caller before its local dependency,
	// matching a valid map iteration order that previously failed to compile.
	err = materializePipelines(req, []pipelineDefinition{
		{name: "local_chain", source: "pipeline.local_support"},
		{name: "local_support", source: "runtime.const"},
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, compileOrder, []string{
		"pipeline.local_chain",
		"pipeline.local_support",
		"pipeline.local_chain",
	})
	for _, name := range []string{"pipeline.local_chain", "pipeline.local_support"} {
		if _, ok := resolver.Action(name); !ok {
			t.Errorf("materialized action %q is not registered", name)
		}
	}
}
