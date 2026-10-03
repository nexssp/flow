package runner_test

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

func ladderConfig(t *testing.T) runner.Config {
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

func ladderRun(t *testing.T, cfg runner.Config, src string) (runner.Execution, error) {
	t.Helper()
	return runner.Execute(context.Background(), cfg, src, "test.nflow", nil)
}

func ladderSleep(ms int) string {
	return "sleep @{ duration_ms: " + strconv.Itoa(ms) + " }"
}

func TestLadder_AtomWinsOverEveryInheritedLayer(t *testing.T) {
	cfg := ladderConfig(t)
	// Every inherited layer says 10s; the atom says 1ms. The sleep
	// can only fail if the atom-local value won. Failure = correct.
	src := `
@profile base :timeout=10s

@pipeline work :profile=base :timeout=10s
  @scope :timeout=10s {
    @scope :timeout=10s {
      sleep:timeout=1ms @{ duration_ms: 200 }
    }
  }
@end

{} -> pipeline.work
`
	_, err := ladderRun(t, cfg, src)
	ktest.RequireCondition(t, err != nil,
		"atom-local :timeout must win over every inherited layer")
}

func TestLadder_InnerScopeBeatsPipeline(t *testing.T) {
	cfg := ladderConfig(t)
	src := `
@pipeline work :timeout=1ms
  @scope :timeout=1s {
    ` + ladderSleep(50) + `
  }
@end

{} -> pipeline.work
`
	_, err := ladderRun(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestLadder_PipelineBeatsProfile(t *testing.T) {
	cfg := ladderConfig(t)
	src := `
@profile base :timeout=1ms

@pipeline work :profile=base :timeout=1s
  ` + ladderSleep(50) + `
@end

{} -> pipeline.work
`
	_, err := ladderRun(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestBoundary_OuterScopeDoesNotCrossPipeline(t *testing.T) {
	cfg := ladderConfig(t)
	// Outer scope carries :timeout=1ms; pipeline body sleeps 50ms.
	// Success proves the scope did not cross into the pipeline body.
	src := `
@scope :timeout=1ms {
  @pipeline work
    ` + ladderSleep(50) + `
  @end
}

{} -> pipeline.work
`
	_, err := ladderRun(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestBoundary_InnerScopeReachesPipelineBody(t *testing.T) {
	cfg := ladderConfig(t)
	src := `
@pipeline work
  @scope :timeout=1ms {
    ` + ladderSleep(200) + `
  }
@end

{} -> pipeline.work
`
	_, err := ladderRun(t, cfg, src)
	ktest.RequireCondition(t, err != nil,
		"inner scope must reach body atom")
}
