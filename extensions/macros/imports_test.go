package macros

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	constants "github.com/nexssp/flow/extensions/constants"
	includeext "github.com/nexssp/flow/extensions/include"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	schemaext "github.com/nexssp/flow/extensions/schema"
	scopeext "github.com/nexssp/flow/extensions/scope"
	symbolsext "github.com/nexssp/flow/extensions/symbols"
	"github.com/nexssp/flow/runner"
)

func macroImportConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]compiler.Bundle{
		includeext.Bundle(nil),
		Bundle(nil),
		pipelineext.Bundle(nil),
		runtimeext.Bundle(nil),
		schemaext.Bundle(nil),
		symbolsext.Bundle(nil),
		constants.Bundle(nil),
		scopeext.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func writeMacroImportFixture(t *testing.T, dir, name, source string) {
	t.Helper()
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600))
}

func TestOrdinaryIncludeExportsMacroAlongsidePipelinesSchemasAndSymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMacroImportFixture(t, dir, "helpers.nflow", `@macro echo(value: Expr) {
  runtime.const @{ value: $value }
}
`)
	writeMacroImportFixture(t, dir, "reporting.nflow", `@const PRIVATE_REPORT = "module-only"
@profile report_private :timeout=2s
@symbols output {
  text: "from-reporting"
}
@schema StatsRequest {
  Value string
}
@include ./helpers.nflow AS helpers
@macro stats_config(field: Identifier) {
  $field: "configured"
}
@macro echo(value: Expr) {
  runtime.const @{ value: $value }
}
@macro through_local(value: Expr) {
  @echo($value)
}
@macro through_nested(value: Expr) {
  helpers.@echo($value)
}
@pipeline foo
  runtime.noop
@end
`)

	rootFile := filepath.Join(dir, "main.nflow")
	cfg := macroImportConfig(t)
	compiled, err := runner.Compile(t.Context(), cfg,
		"@include ./reporting.nflow AS reporting\nruntime.const @{ reporting.@stats_config(tree_only) }",
		rootFile)
	ktest.RequireNoError(t, err)

	out, err := compiled.Program.DoAny(t.Context(), nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, out, any(map[string]any{"tree_only": "configured"}))

	schema, ok := schemaext.ByName(compiled.Meta, "reporting.StatsRequest")
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, schema.Name, "reporting.StatsRequest")
	symbol, ok := compiler.ResolveSymbolValue(compiled.Meta, "reporting.output.text")
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, symbol, "from-reporting")
	_, ok = compiled.Resolver.Action("reporting.pipeline.foo")
	ktest.RequireTrue(t, ok)

	imports := compiler.SourceImports(compiled.Meta)
	ktest.RequireLen(t, imports, 1)
	childMeta := imports[0].Source.Metadata()
	ktest.RequireTrue(t, childMeta["constants"] != nil)
	ktest.RequireTrue(t, childMeta["scope.profiles"] != nil)
	_, hasRootConstants := compiled.Meta["constants"]
	_, hasRootProfiles := compiled.Meta["scope.profiles"]
	_, hasMergedMacroDeclarations := compiled.Meta[DeclarationKey]
	ktest.RequireFalse(t, hasRootConstants)
	ktest.RequireFalse(t, hasRootProfiles)
	ktest.RequireFalse(t, hasMergedMacroDeclarations)

	for _, tc := range []struct {
		source string
		want   any
	}{
		{source: "@include ./reporting.nflow AS reporting\nreporting.@through_local(\"local\")", want: "local"},
		{source: "@include ./reporting.nflow AS reporting\nreporting.@through_nested(\"nested\")", want: "nested"},
	} {
		call, compileErr := runner.Compile(t.Context(), cfg, tc.source, rootFile)
		ktest.RequireNoError(t, compileErr)
		got, runErr := call.Program.DoAny(t.Context(), nil)
		ktest.RequireNoError(t, runErr)
		ktest.RequireEqual(t, got, tc.want)
	}
}

func TestIncludedMacrosWithSameLeafNameRemainUnambiguous(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMacroImportFixture(t, dir, "first.nflow", `@macro shared() { runtime.const @{ value: "first" } }
`)
	writeMacroImportFixture(t, dir, "second.nflow", `@macro shared() { runtime.const @{ value: "second" } }
`)
	rootFile := filepath.Join(dir, "main.nflow")
	prefix := `@include ./first.nflow AS first
@include ./second.nflow AS second
@macro shared() { runtime.const @{ value: "local" } }
`
	cfg := macroImportConfig(t)
	for _, tc := range []struct {
		call string
		want string
	}{
		{call: "first.@shared()", want: "first"},
		{call: "second.@shared()", want: "second"},
		{call: "@shared()", want: "local"},
	} {
		compiled, err := runner.Compile(t.Context(), cfg, prefix+tc.call, rootFile)
		ktest.RequireNoError(t, err)
		out, err := compiled.Program.DoAny(t.Context(), nil)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, out, any(tc.want))
	}
}

func TestImportedMacroConflictsAreRejectedByDeclarationAndNamespaceChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMacroImportFixture(t, dir, "first.nflow", `@macro shared() { runtime.noop }
`)
	writeMacroImportFixture(t, dir, "second.nflow", `@macro shared() { runtime.noop }
`)
	rootFile := filepath.Join(dir, "main.nflow")
	cfg := macroImportConfig(t)

	_, err := runner.Compile(t.Context(), cfg,
		"@include ./first.nflow AS reporting\n@include ./second.nflow AS reporting\nruntime.noop",
		rootFile)
	ktest.RequireErrorContains(t, err, `duplicate declaration @include "reporting"`)
	ktest.RequireErrorContains(t, err, "first declared at")

	writeMacroImportFixture(t, dir, "duplicate.nflow", `@macro repeated() { runtime.noop }
@macro repeated() { runtime.noop }
`)
	_, err = runner.Compile(t.Context(), cfg, "@include ./duplicate.nflow AS duplicate\nruntime.noop", rootFile)
	ktest.RequireErrorContains(t, err, `duplicate declaration @macro "repeated"`)
	ktest.RequireErrorContains(t, err, "first declared at")

	_, err = runner.Compile(t.Context(), cfg,
		"@symbols reporting { local: \"root\" }\n@include ./first.nflow AS reporting\nruntime.noop",
		rootFile)
	ktest.RequireErrorContains(t, err, `duplicate declaration @include "reporting"`)
}

func TestImportedActionRefUsesNormalCompilerResolver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMacroImportFixture(t, dir, "actions.nflow", `@macro invoke(action: ActionRef) { $action }
`)
	rootFile := filepath.Join(dir, "main.nflow")
	cfg := macroImportConfig(t)

	_, err := runner.Compile(t.Context(), cfg,
		"@include ./actions.nflow AS reporting\nreporting.@invoke(runtime.noop)", rootFile)
	ktest.RequireNoError(t, err)

	_, err = runner.Compile(t.Context(), cfg,
		"@include ./actions.nflow AS reporting\nreporting.@invoke(unregistered.capability)", rootFile)
	ktest.RequireErrorContains(t, err, "capability unregistered.capability not registered")
}
