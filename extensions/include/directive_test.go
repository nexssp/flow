package include

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
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

// runInclude provides only the import callback used by the current include
// contract. Filesystem resolution belongs to SourceLoader, not the directive.
func runInclude(tb testing.TB, line string, importSource func(string, string) error) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	body := []string{line}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: body, Body: body, I: 0, Out: out, File: "<test>",
		Table: includeTestDirectiveTable(), Import: importSource,
	})
	return out, err
}

func TestIncludeImportsRoutePipelinesAndKeepsKernelBindings(t *testing.T) {
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
	ktest.RequireEqual(t, len(compiler.SourceImports(compiled.Meta)), 2)
	_, hasParentPipelines := compiled.Meta["pipelines"]
	ktest.RequireFalse(t, hasParentPipelines)

	for _, tc := range []struct {
		file, actionName, pipelineName, mod, method, path string
	}{
		{file: "users.nflow", actionName: "users.pipeline.users", pipelineName: "users", mod: "route=GET /users", method: "GET", path: "/users"},
		{file: "orders.nflow", actionName: "orders.pipeline.orders", pipelineName: "orders", mod: "route=POST /orders", method: "POST", path: "/orders"},
	} {
		imported, ok := compiled.Resolver.Action(tc.actionName)
		ktest.RequireTrue(t, ok)
		bindings := imported.GetBindings()
		ktest.RequireLen(t, bindings, 1)
		binding, ok := bindings[0].(testRouteBinding)
		ktest.RequireTrue(t, ok)
		ktest.RequireEqual(t, binding, testRouteBinding{Method: tc.method, Path: tc.path})

		var childMeta map[string]any
		for _, sourceImport := range compiler.SourceImports(compiled.Meta) {
			name, _ := sourceImport.Source.SourceIdentity()
			if filepath.Base(name) == tc.file {
				childMeta = sourceImport.Source.Metadata()
			}
		}
		// The imported source owns its declarations and modifier provenance.
		ktest.RequireTrue(t, childMeta != nil)
		modifiers, ok := childMeta["pipeline_modifiers"].(map[string][]string)
		ktest.RequireTrue(t, ok)
		ktest.RequireEqual(t, modifiers[tc.pipelineName], []string{tc.mod})
		sources, ok := childMeta["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
		ktest.RequireTrue(t, ok)
		ktest.RequireEqual(t, sources[tc.pipelineName], []compiler.ModifierSource{{Kind: "pipeline", Label: tc.pipelineName}})
	}
}

func TestDirective_ImportUsesDefaultBasenameNamespace(t *testing.T) {
	t.Parallel()
	var target, namespace string
	_, err := runInclude(t, `@include "shared/child.nflow"`, func(gotTarget, gotNamespace string) error {
		target, namespace = gotTarget, gotNamespace
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, target, "shared/child.nflow")
	ktest.RequireEqual(t, namespace, "child")
}

func TestDirective_ImportUsesExplicitNamespace(t *testing.T) {
	t.Parallel()
	var target, namespace string
	_, err := runInclude(t, `@include child.nflow AS billing # shared module`, func(gotTarget, gotNamespace string) error {
		target, namespace = gotTarget, gotNamespace
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, target, "child.nflow")
	ktest.RequireEqual(t, namespace, "billing")
}

func TestDirective_AbsolutePathPassedToSourceLoader(t *testing.T) {
	t.Parallel()
	absolute := filepath.Join(t.TempDir(), "child.nflow")
	var captured string
	_, err := runInclude(t, `@include "`+absolute+`"`, func(target, _ string) error {
		captured = target
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, captured, absolute)
}

func TestDirective_RelativePathPassedUnresolvedToSourceLoader(t *testing.T) {
	t.Parallel()
	var captured string
	_, err := runInclude(t, `@include child.nflow`, func(target, _ string) error {
		captured = target
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, captured, "child.nflow")
}

func TestDirective_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include`, nil)
	ktest.RequireErrorContains(t, err, "requires a file path")
}

func TestDirective_NoImport(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include "x.nflow"`, nil)
	ktest.RequireErrorContains(t, err, "source imports are not available")
}

func TestDirective_RejectsUnexpectedTextAfterNamespace(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include child.nflow AS child trailing`, func(string, string) error { return nil })
	ktest.RequireErrorContains(t, err, "unexpected text after namespace")
}

// Unit fixtures install the directive owners needed by their compiler paths.
func includeTestDirectiveTable() *compiler.DirectiveTable {
	bundles := []compiler.Bundle{Bundle(nil), pipelineext.Bundle(nil)}
	var directives []compiler.Directive
	for i := range bundles {
		directives = append(directives, bundles[i].Directives...)
	}
	return compiler.NewDirectiveTable(directives...)
}

func TestIncludeMaterializesForwardLocalPipelineReferences(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	childFile := filepath.Join(dir, "billing.nflow")
	rootFile := filepath.Join(dir, "root.nflow")
	child := `@pipeline local_chain
  pipeline.local_support
@end

@pipeline local_support
  runtime.const @{ value: "included" }
@end
`
	ktest.RequireNoError(t, os.WriteFile(childFile, []byte(child), 0o600))

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		Bundle(nil),
		pipelineext.Bundle(nil),
		runtimeext.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	source := "@include ./billing.nflow\nbilling.pipeline.local_chain"
	result, err := runner.Execute(t.Context(), cfg, source, rootFile, nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, result.Output, "included")
}
