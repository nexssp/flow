package runner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
)

func TestCompileAndExecute_PreprocessRootOnceAndPreserveMetadata(t *testing.T) {
	var rootDirectiveCalls, pipelineDirectiveCalls atomic.Int32
	var rootPreprocessCalls, pipelinePreprocessCalls atomic.Int32
	newConfig := func() Config {
		cfg, err := BuildConfig(native.Bundles())
		if err != nil {
			t.Fatal(err)
		}
		directives := cfg.Directives.All()
		directives = append(directives, core.Directive{
			Name: "testmark",
			Handler: func(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
				switch req.File {
				case "prepared.nflow":
					rootDirectiveCalls.Add(1)
					req.Out["test_marker"] = "root"
				case "pipeline.local":
					pipelineDirectiveCalls.Add(1)
					req.Out["test_marker"] = "pipeline"
				}
				return core.DirectiveRes{Next: req.I + 1}, nil
			},
		})
		cfg.Directives = core.NewDirectiveTable(directives...)
		cfg.CompileOpts = append(cfg.CompileOpts, core.WithOnPreprocess(func(meta map[string]any) core.PreprocessContributions {
			switch meta["test_marker"] {
			case "root":
				rootPreprocessCalls.Add(1)
			case "pipeline":
				pipelinePreprocessCalls.Add(1)
			}
			return core.PreprocessContributions{}
		}))
		return cfg
	}

	src := `@config:strict=true
@macro greet() { runtime.const @{ value: "prepared root" } }
@testmark
@pipeline local
  @testmark
  runtime.noop
@end
@greet()`
	assertOnce := func() {
		t.Helper()
		if got := rootDirectiveCalls.Load(); got != 1 {
			t.Fatalf("root directive calls = %d, want 1", got)
		}
		if got := pipelineDirectiveCalls.Load(); got != 1 {
			t.Fatalf("pipeline directive calls = %d, want 1", got)
		}
		if got := rootPreprocessCalls.Load(); got != 1 {
			t.Fatalf("root preprocess callbacks = %d, want 1", got)
		}
		if got := pipelinePreprocessCalls.Load(); got != 1 {
			t.Fatalf("pipeline preprocess callbacks = %d, want 1", got)
		}
	}

	compiled, err := Compile(context.Background(), newConfig(), src, "prepared.nflow")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.Program == nil {
		t.Fatal("Compile returned no program")
	}
	if got := compiled.Meta["test_marker"]; got != "root" {
		t.Fatalf("Compile metadata marker = %#v, want root", got)
	}
	assertOnce()

	rootDirectiveCalls.Store(0)
	pipelineDirectiveCalls.Store(0)
	rootPreprocessCalls.Store(0)
	pipelinePreprocessCalls.Store(0)
	ex, err := Execute(context.Background(), newConfig(), src, "prepared.nflow", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := ex.Output, "prepared root"; got != want {
		t.Fatalf("Execute output = %#v, want %q", got, want)
	}
	if got := ex.Meta["test_marker"]; got != "root" {
		t.Fatalf("Execute metadata marker = %#v, want root", got)
	}
	assertOnce()
}

func TestCompile_DeclarationOnlySourceHasNoProgram(t *testing.T) {
	cfg, err := BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}

	compiled, err := Compile(context.Background(), cfg, `@pipeline only
  runtime.noop
@end`, "declarations.nflow")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.Program != nil {
		t.Fatalf("declaration-only source returned program %T, want nil", compiled.Program)
	}
	pipelines, _ := compiled.Meta["pipelines"].(map[string]string)
	if _, ok := pipelines["only"]; !ok {
		t.Fatalf("pipeline declaration missing from metadata: %#v", compiled.Meta)
	}
}
