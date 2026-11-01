package macros

import (
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/runner"
)

func TestImportedPipelineDuplicateCanonicalNameIsRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMacroImportFixture(t, dir, "pipelines.nflow", `@pipeline repeated
  runtime.noop
@end
@pipeline pipeline.repeated
  runtime.noop
@end
`)
	_, err := runner.Compile(t.Context(), macroImportConfig(t),
		"@include ./pipelines.nflow AS reporting\nruntime.noop",
		filepath.Join(dir, "main.nflow"))
	ktest.RequireErrorContains(t, err, `duplicate declaration @pipeline "pipeline.repeated"`)
	ktest.RequireErrorContains(t, err, "first declared at")
}

func TestLocalPipelineCannotShadowImportedNamespacePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMacroImportFixture(t, dir, "reporting.nflow", `@pipeline foo
  runtime.noop
@end
`)
	source := `@include ./reporting.nflow AS reporting
@pipeline reporting.pipeline.foo
  runtime.noop
@end
runtime.noop`
	_, err := runner.Compile(t.Context(), macroImportConfig(t), source, filepath.Join(dir, "main.nflow"))
	ktest.RequireErrorContains(t, err, "canonical collision")
	ktest.RequireErrorContains(t, err, "reporting.pipeline.foo")
}

func TestMacroExpansionCannotCreateDeclarations(t *testing.T) {
	t.Parallel()
	_, err := newTestParser(t, map[string]Declaration{
		"declare_pipeline": {
			Name: "declare_pipeline",
			Body: "@pipeline generated",
		},
	}, `@declare_pipeline()`).Parse()
	ktest.RequireErrorContains(t, err, "unknown macro @pipeline")
}
