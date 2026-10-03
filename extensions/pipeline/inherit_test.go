package pipeline_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/scope"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func inheritConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		projection.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		scope.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func inheritRun(t *testing.T, cfg runner.Config, src string) (runner.Execution, error) {
	t.Helper()
	return runner.Execute(context.Background(), cfg, src, "test.nflow", nil)
}

func inheritSleep(ms int) string {
	return "sleep @{ duration_ms: " + strconv.Itoa(ms) + " }"
}

func TestPipeline_TimeoutReachesBody(t *testing.T) {
	cfg := inheritConfig(t)
	src := `
@pipeline work :timeout=1ms
  ` + inheritSleep(200) + `
@end

{} -> pipeline.work
`
	_, err := inheritRun(t, cfg, src)
	ktest.RequireCondition(t, err != nil,
		"pipeline :timeout must reach body atom")
}

func TestPipeline_ProfileReachesBody(t *testing.T) {
	cfg := inheritConfig(t)
	src := `
@profile fast :timeout=1ms

@pipeline work :profile=fast
  ` + inheritSleep(200) + `
@end

{} -> pipeline.work
`
	_, err := inheritRun(t, cfg, src)
	ktest.RequireCondition(t, err != nil,
		"profile :timeout must reach body atom")
}

func TestPipeline_LocalBeatsProfile(t *testing.T) {
	cfg := inheritConfig(t)
	src := `
@profile base :timeout=1ms

@pipeline work :profile=base :timeout=1s
  ` + inheritSleep(50) + `
@end

{} -> pipeline.work
`
	_, err := inheritRun(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestPipeline_MetadataStaysOnWrapper(t *testing.T) {
	cfg := inheritConfig(t)
	src := `
@pipeline work :tag="wrapper-only"
  const @{ value: "ok" }
@end

{} -> pipeline.work
`
	ex, err := inheritRun(t, cfg, src)
	ktest.RequireNoError(t, err)

	act, found := ex.Resolver.Action("pipeline.work")
	ktest.RequireCondition(t, found, "pipeline.work not mounted")

	hasTag := false
	for _, tag := range act.Describe().Tags {
		if tag == "wrapper-only" {
			hasTag = true
		}
	}
	ktest.RequireCondition(t, hasTag,
		"wrapper must carry :tag, got %v", act.Describe().Tags)
}
