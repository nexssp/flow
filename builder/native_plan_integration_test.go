package builder

import (
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

type virtualBuildLoader struct {
	calls int
}

func (loader *virtualBuildLoader) Load(_, target string) (data []byte, resolved string, err error) {
	loader.calls++
	return []byte("@symbols virtual {\nstate: \"READY\"\n}\n"), target, nil
}

func TestBuildSourcesKeepsCallerSourceLoader(t *testing.T) {
	t.Parallel()
	loader := &virtualBuildLoader{}
	ctx := compiler.WithSourceLoader(t.Context(), loader)
	path := filepath.Join(t.TempDir(), "main.nflow")
	sources, err := buildSourcesFrom(ctx, path, "@include virtual-not-on-disk.nflow\nruntime.noop")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, loader.calls, 1)
	plan, err := nativeBundlesForSources(sources, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireTrue(t, planHasBundle(plan, "symbols"))
}

func TestNativePlanSeesExpandedActionInCapturedPipeline(t *testing.T) {
	t.Parallel()
	plan := planSourceFixture(t, "@const ACTION = \"render.markdown\"\n@pipeline child\n{} -> ${ACTION}\n@end\n{} -> pipeline.child", false)
	ktest.RequireTrue(t, planHasBundle(plan, "render"))
	ktest.RequireTrue(t, planHasBundle(plan, "pipeline"))
}

// Captured pipeline bodies contribute their nested modifiers to bundle selection.
func TestNativePlanSelectsRetryFromCapturedPipelineBody(t *testing.T) {
	t.Parallel()
	sources := []compiler.BuildSource{{
		Clean:      "{} -> pipeline.child",
		Directives: []string{"pipeline"},
		Metadata: map[string]any{
			"pipelines": map[string]string{"child": "runtime.noop :retry=3"},
		},
	}}
	plan, err := nativeBundlesForSources(sources, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireTrue(t, planHasBundle(plan, "pipeline"))
	ktest.RequireTrue(t, planHasBundle(plan, "retry"))
}

func TestSourceRequiresDoesNotActivateSelftestkitFromText(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"# cov.echo\nruntime.noop", "{value: \"cov.echo\"} -> runtime.noop", "cov.echo"} {
		path := filepath.Join(t.TempDir(), "main.nflow")
		writeSourceGraphFixture(t, path, source)
		reqs, err := SourceRequires(t.Context(), path)
		ktest.RequireNoError(t, err)
		ktest.RequireFalse(t, hasRequirement(reqs, "selftestkit"))
	}
}
