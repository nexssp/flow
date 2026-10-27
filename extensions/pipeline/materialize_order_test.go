package pipeline_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func pipelineTestConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	return cfg
}

func TestMaterializeCompilesReverseAlphabeticalDependencies(t *testing.T) {
	cfg := pipelineTestConfig(t)
	source := `@pipeline a
  pipeline.b
@end
@pipeline b
  pipeline.c
@end
@pipeline c
  runtime.const @{ value: "compiled in dependency order" }
@end
pipeline.a
`

	result, err := runner.Execute(t.Context(), cfg, source, "reverse-dependency.nflow", nil)
	if err != nil {
		t.Fatalf("execute reverse-ordered pipeline chain: %v", err)
	}
	if result.Output != "compiled in dependency order" {
		t.Fatalf("pipeline chain output = %#v, want %q", result.Output, "compiled in dependency order")
	}
}

func TestMaterializeRetriesDistributeMapForwardPipelineCapability(t *testing.T) {
	cfg := pipelineTestConfig(t)
	source := `@pipeline a_process_batch
  distribute.map @{ action: pipeline.normalize_order, items: [1] }
@end
@pipeline normalize_order
  runtime.const @{ value: "normalized" }
@end
pipeline.a_process_batch
`
	compiled, err := runner.Compile(t.Context(), cfg, source, "pipeline-capability-forward-reference.nflow")
	if err != nil {
		t.Fatalf("compile distribute.map with a forward local pipeline action: %v", err)
	}
	for _, name := range []string{"pipeline.a_process_batch", "pipeline.normalize_order"} {
		if _, ok := compiled.Resolver.Action(name); !ok {
			t.Errorf("compiled resolver does not contain %q", name)
		}
	}
}

func TestMaterializeNoProgressReportsStableJoinedDiagnostics(t *testing.T) {
	cfg := pipelineTestConfig(t)
	source := `@pipeline a
  pipeline.b
@end
@pipeline b
  pipeline.a
@end
@pipeline y
  (
@end
@pipeline z
  missing.action
@end
pipeline.a
`

	// A regression in the retry loop must fail the test instead of leaving the
	// package test process stuck indefinitely.
	compile := func() (string, error) {
		t.Helper()
		type outcome struct {
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			_, err := runner.Compile(context.Background(), cfg, source, "stalled-dependencies.nflow")
			done <- outcome{err: err}
		}()
		select {
		case got := <-done:
			if got.err == nil {
				t.Fatal("compile succeeded despite cyclic and invalid sub-pipelines")
			}
			return got.err.Error(), got.err
		case <-time.After(5 * time.Second):
			t.Fatal("materialization did not terminate after dependency compilation stopped making progress")
			return "", nil
		}
	}

	first, err := compile()
	if err == nil {
		t.Fatal("expected materialization diagnostics")
	}
	if !strings.Contains(first, "unable to materialize sub-pipelines (no progress)") {
		t.Fatalf("error lacks no-progress summary: %v", err)
	}
	for _, name := range []string{`compile sub-pipeline "a"`, `compile sub-pipeline "b"`, `compile sub-pipeline "y"`, `compile sub-pipeline "z"`} {
		if !strings.Contains(first, name) {
			t.Errorf("joined diagnostics lack %q: %v", name, err)
		}
	}
	if !strings.Contains(first, "missing.action") {
		t.Errorf("diagnostics lack the unknown action: %v", err)
	}

	for range 2 {
		got, repeatErr := compile()
		if repeatErr == nil {
			t.Fatal("expected materialization diagnostics on repeated compile")
		}
		if got != first {
			t.Fatalf("diagnostics changed between compilations:\nfirst: %s\nagain: %s", first, got)
		}
	}
}
