package include

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/constants"
	"github.com/nexssp/flow/extensions/macros"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/require"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/symbols"
	"github.com/nexssp/flow/runner"
)

type testRouteBinding struct {
	Method string
	Path   string
}

func testRouteBundle() compiler.Bundle {
	routes := map[string]testRouteBinding{
		"GET /users":   {Method: "GET", Path: "/users"},
		"POST /orders": {Method: "POST", Path: "/orders"},
	}
	modifier := compiler.String("route", func(b *action.Builder[any, any], raw string) *action.Builder[any, any] {
		return b.Route(routes[raw])
	})
	return compiler.Bundle{ID: "test_route_binding", Modifiers: []compiler.Modifier{modifier}}
}

// runInclude simulates the Recurse callback with a canned result. The
// real recursion is exercised through the runner; here we test only
// this directive's parsing and merge logic.
func runInclude(tb testing.TB, line, baseDir string, recurse func(string) (string, map[string]any, error)) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	body := []string{line}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines:   body,
		Body:    body,
		I:       0,
		Out:     out,
		File:    "<test>",
		BaseDir: baseDir,
		Table:   includeTestDirectiveTable(),
		Recurse: recurse,
	})
	return out, err
}

func TestDirective_MergesPipelines(t *testing.T) {
	t.Parallel()
	out, err := runInclude(t, `@include "child.nflow"`, "", func(_ string) (string, map[string]any, error) {
		return `runtime.const @{ value: "x" }`, map[string]any{
			"pipelines": map[string]string{"p": `runtime.const @{ value: "x" }`},
		}, nil
	})
	ktest.RequireNoError(t, err)

	pipelines, _ := out["pipelines"].(map[string]string)
	ktest.RequireEqual(t, pipelines["p"], `runtime.const @{ value: "x" }`)
}

func TestInclude_MergesRoutePipelinesAndKeepsKernelBindings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	usersFile := filepath.Join(dir, "users.nflow")
	ordersFile := filepath.Join(dir, "orders.nflow")
	rootFile := filepath.Join(dir, "root.nflow")
	ktest.RequireNoError(t, os.WriteFile(usersFile, []byte("@pipeline users:route=\"GET /users\"\n  runtime.noop\n@end\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(ordersFile, []byte("@pipeline orders:route=\"POST /orders\"\n  runtime.noop\n@end\n"), 0o600))

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		Bundle(nil),
		pipelineext.Bundle(nil),
		runtimeext.Bundle(nil),
		testRouteBundle(),
	})
	ktest.RequireNoError(t, err)

	compiled, err := runner.Compile(t.Context(), cfg, "@include ./users.nflow\n@include ./orders.nflow\nruntime.noop", rootFile)
	ktest.RequireNoError(t, err)

	modifiers, ok := compiled.Meta["pipeline_modifiers"].(map[string][]string)
	ktest.RequireTrue(t, ok)
	sources, ok := compiled.Meta["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
	ktest.RequireTrue(t, ok)
	for _, tc := range []struct {
		name   string
		mod    string
		method string
		path   string
	}{
		{name: "users", mod: "route=GET /users", method: "GET", path: "/users"},
		{name: "orders", mod: "route=POST /orders", method: "POST", path: "/orders"},
	} {
		ktest.RequireEqual(t, modifiers[tc.name], []string{tc.mod})
		ktest.RequireEqual(t, sources[tc.name], []compiler.ModifierSource{{Kind: "pipeline", Label: tc.name}})

		mounted, ok := compiled.Resolver.Action("pipeline." + tc.name)
		ktest.RequireTrue(t, ok)
		bindings := mounted.GetBindings()
		ktest.RequireLen(t, bindings, 1)
		binding, ok := bindings[0].(testRouteBinding)
		ktest.RequireTrue(t, ok)
		ktest.RequireEqual(t, binding, testRouteBinding{Method: tc.method, Path: tc.path})
	}
}

func TestMergeIncludedMeta_ClearsModifiersWhenPipelineIsReplacedWithoutThem(t *testing.T) {
	t.Parallel()
	parent := map[string]any{
		"pipelines":          map[string]string{"users": "old body"},
		"pipeline_modifiers": map[string][]string{"users": {"route=GET /users"}},
		"pipeline_modifier_sources": map[string][]compiler.ModifierSource{
			"users": {{Kind: "pipeline", Label: "users"}},
		},
	}
	included := map[string]any{
		"pipelines": map[string]string{"users": "replacement body"},
	}

	ktest.RequireNoError(t, mergeIncludedMeta(parent, included))
	pipelines, _ := parent["pipelines"].(map[string]string)
	ktest.RequireEqual(t, pipelines["users"], "replacement body")
	modifiers, _ := parent["pipeline_modifiers"].(map[string][]string)
	sources, _ := parent["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
	ktest.RequireEqual(t, len(modifiers), 0)
	ktest.RequireEqual(t, len(sources), 0)
}

func TestMergeIncludedMeta_RejectsUnalignedPipelineModifierSources(t *testing.T) {
	included := map[string]any{
		"pipelines":          map[string]string{"users": "body"},
		"pipeline_modifiers": map[string][]string{"users": {"tag=fast", "status=201"}},
		"pipeline_modifier_sources": map[string][]compiler.ModifierSource{
			"users": {{Kind: "pipeline", Label: "users"}},
		},
	}
	ktest.RequireErrorContains(t, mergeIncludedMeta(map[string]any{}, included), "2 modifiers but 1 modifier sources")
}

func TestDirective_ConcatenatesRequire(t *testing.T) {
	t.Parallel()
	out, err := runInclude(t, `@include "child.nflow"`, "", func(_ string) (string, map[string]any, error) {
		return "", map[string]any{"require": []require.Requirement{{Import: "a"}, {Import: "b"}}}, nil
	})
	ktest.RequireNoError(t, err)

	req, _ := out["require"].([]require.Requirement)
	ktest.RequireEqual(t, req, []require.Requirement{{Import: "a"}, {Import: "b"}})
}

func TestDirective_ParentValueWins(t *testing.T) {
	t.Parallel()
	out := map[string]any{"description": "parent"}
	body := []string{`@include "child.nflow"`}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: body, Body: body, I: 0, Out: out, File: "<test>",
		Recurse: func(_ string) (string, map[string]any, error) {
			return "", map[string]any{"description": "child"}, nil
		},
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, out["description"], "parent")
}

func TestDirective_AbsolutePathPassthrough(t *testing.T) {
	t.Parallel()
	absolute := filepath.Join(t.TempDir(), "child.nflow")
	var captured string
	_, err := runInclude(t, `@include "`+absolute+`"`, "/some/base", func(p string) (string, map[string]any, error) {
		captured = p
		return "", nil, nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, captured, absolute)
}

func TestDirective_RelativeToBaseDir(t *testing.T) {
	t.Parallel()
	var captured string
	_, err := runInclude(t, `@include "child.nflow"`, "/tmp/base", func(p string) (string, map[string]any, error) {
		captured = p
		return "", nil, nil
	})
	ktest.RequireNoError(t, err)

	want, _ := filepath.Abs("/tmp/base/child.nflow")
	ktest.RequireEqual(t, captured, want)
}

func TestDirective_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include`, "", nil)
	ktest.RequireErrorContains(t, err, "requires a file path")
}

func TestDirective_NoRecurse(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include "x.nflow"`, "", nil)
	ktest.RequireErrorContains(t, err, "recurse is not available")
}

func TestMergeIncludedMeta_ConcatenatesMacros(t *testing.T) {
	parent := map[string]any{
		"macros": []macros.Declaration{{Name: "parent_macro"}},
	}
	included := map[string]any{
		"macros": []macros.Declaration{{Name: "included_macro"}},
	}

	ktest.RequireNoError(t, mergeIncludedMeta(parent, included))

	got, ok := parent["macros"].([]macros.Declaration)
	ktest.RequireTrue(t, ok)
	ktest.RequireLen(t, got, 2)
	ktest.RequireEqual(t, got[0].Name, "parent_macro")
	ktest.RequireEqual(t, got[1].Name, "included_macro")
}

func TestMergeIncludedMeta_MacrosFromSecondIncludeSurvive(t *testing.T) {
	parent := map[string]any{
		"macros": []macros.Declaration{{Name: "first"}},
	}
	second := map[string]any{
		"macros": []macros.Declaration{{Name: "second"}},
	}

	ktest.RequireNoError(t, mergeIncludedMeta(parent, second))

	got, ok := parent["macros"].([]macros.Declaration)
	ktest.RequireTrue(t, ok)
	ktest.RequireLen(t, got, 2)
	ktest.RequireEqual(t, got[1].Name, "second")
}

func TestMergeIncludedMeta_MergesDistinctSymbolNamespaces(t *testing.T) {
	parent := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"first": {Name: "first", Symbols: map[string]any{"value": "one"}},
		},
	}
	included := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"second": {Name: "second", Symbols: map[string]any{"value": "two"}},
		},
	}
	ktest.RequireNoError(t, mergeIncludedMeta(parent, included))
	got, _ := parent["symbols"].(map[string]compiler.SymbolNamespace)
	ktest.RequireEqual(t, got["first"].Symbols["value"], "one")
	ktest.RequireEqual(t, got["second"].Symbols["value"], "two")
}

func TestMergeIncludedMeta_RejectsDuplicateSymbolNamespace(t *testing.T) {
	parent := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"route": {Name: "route", Symbols: map[string]any{"status": "parent"}},
		},
	}
	included := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"route": {Name: "route", Symbols: map[string]any{"status": "included"}},
		},
	}
	ktest.RequireErrorContains(t, mergeIncludedMeta(parent, included), "duplicate @symbols namespace \"route\"")
	got, _ := parent["symbols"].(map[string]compiler.SymbolNamespace)
	ktest.RequireEqual(t, got["route"].Symbols["status"], "parent")
}

// Unit fixtures install exactly the metadata owners they exercise. Production
// obtains the same contributions from its active bundle set.
func includeTestDirectiveTable() *compiler.DirectiveTable {
	var directives []compiler.Directive
	bundles := []compiler.Bundle{Bundle(nil), pipelineext.Bundle(nil), constants.Bundle(nil), require.Bundle(nil), macros.Bundle(nil), symbols.Bundle(nil)}
	for i := range bundles {
		directives = append(directives, bundles[i].Directives...)
	}
	return compiler.NewDirectiveTable(directives...)
}

func mergeIncludedMeta(parent, included map[string]any) error {
	return includeTestDirectiveTable().MergeMetadata(compiler.DirectiveReq{Out: parent}, included)
}
