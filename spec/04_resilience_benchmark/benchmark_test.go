package resiliencebenchmark

import (
	"context"
	"embed"
	"testing"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

//go:embed baseline.nflow resilient.nflow
var benchmarkSources embed.FS

type workflowFixture struct {
	name   string
	path   string
	source string
}

func loadBenchmarkFixtures(tb testing.TB) (runner.Config, []workflowFixture) {
	tb.Helper()
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		tb.Fatalf("build Flow config: %v", err)
	}

	fixtures := make([]workflowFixture, 0, 2)
	for _, name := range []string{"baseline", "resilient"} {
		path := name + ".nflow"
		source, readErr := benchmarkSources.ReadFile(path)
		if readErr != nil {
			tb.Fatalf("read %s: %v", path, readErr)
		}
		fixtures = append(fixtures, workflowFixture{
			name:   name,
			path:   "spec/04_resilience_benchmark/" + path,
			source: string(source),
		})
	}
	return cfg, fixtures
}

func TestBenchmarkFixturesExecute(t *testing.T) {
	cfg, fixtures := loadBenchmarkFixtures(t)
	ctx := context.Background()
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			execution, err := runner.Execute(ctx, cfg, fixture.source, fixture.path, map[string]any{})
			if err != nil {
				t.Fatalf("execute fixture: %v", err)
			}
			want := fixture.name
			if failures := runner.EvaluateAssertionsContext(ctx, execution.Output, []string{
				`result.status == "ok"`,
				`result.scenario == "` + want + `"`,
			}); len(failures) != 0 {
				t.Fatalf("fixture output %#v failed assertions: %v", execution.Output, failures)
			}
		})
	}
}

// BenchmarkResilienceWorkflow compares a small no-I/O workflow with the same
// workload under timeout/retry/breaker/concurrency middleware. The precompiled
// path calls ExecuteCompiled after compilation; the compile_and_run path includes
// preprocessing and compilation on every iteration. Neither includes CLI startup,
// network latency, storage, or production service-action costs.
func BenchmarkResilienceWorkflow(b *testing.B) {
	b.StopTimer()
	cfg, fixtures := loadBenchmarkFixtures(b)
	ctx := context.Background()

	for _, fixture := range fixtures {
		b.Run("compile_and_run/"+fixture.name, func(b *testing.B) {
			b.StopTimer()
			if _, err := runner.Execute(ctx, cfg, fixture.source, fixture.path, map[string]any{}); err != nil {
				b.Fatalf("warm-up execution: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			for range b.N {
				if _, err := runner.Execute(ctx, cfg, fixture.source, fixture.path, map[string]any{}); err != nil {
					b.Fatalf("execute workflow: %v", err)
				}
			}
		})

		b.Run("compiled_runtime/"+fixture.name, func(b *testing.B) {
			b.StopTimer()
			compiled, err := runner.Compile(ctx, cfg, fixture.source, fixture.path)
			if err != nil {
				b.Fatalf("compile workflow: %v", err)
			}
			if _, err := runner.ExecuteCompiled(ctx, cfg, compiled, fixture.source, fixture.path, map[string]any{}); err != nil {
				b.Fatalf("warm-up execution: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			for range b.N {
				if _, err := runner.ExecuteCompiled(ctx, cfg, compiled, fixture.source, fixture.path, map[string]any{}); err != nil {
					b.Fatalf("execute compiled workflow: %v", err)
				}
			}
		})
	}
}
