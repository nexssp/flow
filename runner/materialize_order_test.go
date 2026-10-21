package runner

import (
	"errors"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestOrderedMaterializersUsesPhasesNotBundleIDs(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := t.Context()
	meta := map[string]any{"value": "original"}
	resolver, err := compiler.NewDynamicResolver()
	ktest.RequireNoError(t, err)
	var compilations int
	req := compiler.MaterializeReq{
		Ctx: ctx, Meta: meta, Resolver: resolver, Alias: "ignored", Introspection: true,
		Compile: func(_, _ string, _ ...string) (action.AnyAction, error) {
			compilations++
			return nil, nil
		},
	}
	record := func(id, alias string) compiler.Materializer {
		return func(got compiler.MaterializeReq) error {
			calls = append(calls, id)
			ktest.RequireEqual(t, got.Alias, alias)
			ktest.RequireCondition(t, got.Ctx == ctx, "context changed")
			ktest.RequireCondition(t, got.Resolver == resolver, "resolver changed")
			ktest.RequireCondition(t, got.Introspection, "introspection flag lost")
			ktest.RequireEqual(t, got.Meta["value"], any("original"))
			got.Meta[id] = true
			_, compileErr := got.Compile("name", "source")
			return compileErr
		}
	}
	bundles := []compiler.Bundle{
		{ID: "compose-first", Alias: "first", MaterializePhase: compiler.MaterializeCompositions, Materialize: record("compose-first", "first")},
		{ID: "pipeline", Materialize: record("pipeline", "")},
		{ID: "no-materializer", MaterializePhase: compiler.MaterializeCompositions},
		{ID: "compose-second", Alias: "second", MaterializePhase: compiler.MaterializeCompositions, Materialize: record("compose-second", "second")},
		{ID: "declare-second", Alias: "provider", Materialize: record("declare-second", "provider")},
	}
	for _, materialize := range orderedMaterializers(bundles) {
		ktest.RequireNoError(t, materialize(req))
	}
	ktest.RequireEqual(t, calls, []string{"pipeline", "declare-second", "compose-first", "compose-second"})
	ktest.RequireEqual(t, compilations, 4)
	for i, id := range []string{"compose-first", "pipeline", "no-materializer", "compose-second", "declare-second"} {
		ktest.RequireEqual(t, bundles[i].ID, id)
	}
	for _, id := range calls {
		ktest.RequireEqual(t, meta[id], any(true))
	}
	ktest.RequireEqual(t, req.Alias, "ignored")
}

func TestOrderedMaterializersEmpty(t *testing.T) {
	t.Parallel()
	ktest.RequireEqual(t, len(orderedMaterializers(nil)), 0)
	ktest.RequireEqual(t, len(orderedMaterializers([]compiler.Bundle{{ID: "empty"}})), 0)
}

func TestOrderedMaterializersPreservesErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("materializer failed")
	materializers := orderedMaterializers([]compiler.Bundle{{
		ID: "failure", Materialize: func(compiler.MaterializeReq) error { return want },
	}})
	ktest.RequireErrorIs(t, materializers[0](compiler.MaterializeReq{Ctx: t.Context()}), want)
}

func TestBuildConfigRejectsInvalidMaterializePhase(t *testing.T) {
	t.Parallel()
	_, err := BuildConfig([]compiler.Bundle{{ID: "invalid", MaterializePhase: -1}})
	ktest.RequireErrorContains(t, err, "invalid materialize phase")
}
