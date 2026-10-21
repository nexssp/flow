package compiler_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestAnalyzeBuildUsageIgnoresCommentsStringsAndSubstrings(t *testing.T) {
	bundles := []compiler.Bundle{{ID: "fs", Libraries: []action.Library{{
		Name: "fs",
		Actions: []action.AnyAction{
			action.New("fs.read", func(context.Context, any) (any, error) { return nil, nil }).Build(),
		},
	}}}}
	for _, source := range []string{
		"# fs.read",
		"// fs.read",
		`{ note: "fs.read" }`,
		"myfs.read",
	} {
		got, err := compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Raw: source}})
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(got), 0)
	}
	got, err := compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Clean: "-> fs.read"}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, []string{"fs"})
}

func TestAnalyzeBuildUsageUsesContributionsAndExactExecutedDirectives(t *testing.T) {
	bundles := []compiler.Bundle{
		{ID: "approval", Directives: []compiler.Directive{{Name: "approval"}}},
		{ID: "child", Directives: []compiler.Directive{{Name: "child"}}, Build: compiler.BuildExtension{Sources: func(meta map[string]any) []string {
			if meta["child"] == "present" {
				return []string{"-> runtime.sleep"}
			}
			return nil
		}}},
		{ID: "runtime", Libraries: []action.Library{{Name: "runtime", Actions: []action.AnyAction{
			action.New("runtime.sleep", func(context.Context, any) (any, error) { return nil, nil }).Build(),
		}}}},
	}
	got, err := compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{
		Raw:        "@pipeline child { -> child.local }",
		Directives: []string{"approval", "child"},
		Metadata:   map[string]any{"child": "present"},
	}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, []string{"approval", "child", "runtime"})

	got, err = compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Raw: "@approval"}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(got), 0)
}

func TestAnalyzeBuildUsageModifiersOperatorsPrimariesAndAlways(t *testing.T) {
	bundles := []compiler.Bundle{
		{ID: "retry", Build: compiler.BuildExtension{Modifiers: []string{"retry"}}},
		{ID: "syntax", Operators: []compiler.Operator{{Meta: compiler.OperatorMeta{Name: "pipe", Token: compiler.TokPipe}}}},
		{ID: "progress", Build: compiler.BuildExtension{Always: true}},
	}
	got, err := compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Raw: "service:retry=3"}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, []string{"retry", "progress"})
	got, err = compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Clean: "left | right"}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, []string{"syntax", "progress"})
	got, err = compiler.AnalyzeBuildUsage(bundles, nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, []string{"progress"})
}

func TestResolveBuildBundlesValidatesDependenciesAndReplacements(t *testing.T) {
	_, err := compiler.ResolveBuildBundles([]compiler.Bundle{{ID: "app", Build: compiler.BuildExtension{Requires: []string{"missing"}}}}, []string{"app"})
	ktest.RequireErrorContains(t, err, "requires missing bundle")

	_, err = compiler.ResolveBuildBundles([]compiler.Bundle{
		{ID: "a", Build: compiler.BuildExtension{Requires: []string{"b"}}},
		{ID: "b", Build: compiler.BuildExtension{Requires: []string{"a"}}},
	}, []string{"a"})
	ktest.RequireErrorContains(t, err, "dependency cycle")

	_, err = compiler.ResolveBuildBundles([]compiler.Bundle{
		{ID: "a", Build: compiler.BuildExtension{Replaces: []string{"b"}}},
		{ID: "b", Build: compiler.BuildExtension{Replaces: []string{"a"}}},
	}, []string{"a", "b"})
	ktest.RequireErrorContains(t, err, "replacement cycle")

	got, err := compiler.ResolveBuildBundles([]compiler.Bundle{
		{ID: "old"},
		{ID: "new", Build: compiler.BuildExtension{Replaces: []string{"old"}}},
	}, []string{"old", "new"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(got), 1)
	ktest.RequireEqual(t, got[0].ID, "new")

	chain := []compiler.Bundle{
		{ID: "a", Build: compiler.BuildExtension{Replaces: []string{"b"}}},
		{ID: "b", Build: compiler.BuildExtension{Replaces: []string{"c"}}},
		{ID: "c"},
	}
	for range 20 {
		got, err = compiler.ResolveBuildBundles(chain, []string{"a", "b", "c"})
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(got), 1)
		ktest.RequireEqual(t, got[0].ID, "a")
	}
}

func TestBuildUsageReadsCanonicalModifierContributions(t *testing.T) {
	bundles := []compiler.Bundle{{ID: "guards", Modifiers: []compiler.Modifier{{Name: "auth"}}}}
	selected, err := compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Raw: "secure.action :auth"}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, selected, []string{"guards"})
	selected, err = compiler.AnalyzeBuildUsage(bundles, []compiler.BuildUsage{{Raw: "auth"}})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(selected), 0)
}
