package flow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/nodes/fsio"
	"github.com/nexssp/kernel/action"
)

func TestResolveParams_EndToEndStreamPipeline(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# doc"), 0o600)

	src := `@config {
  targets: ["` + filepath.ToSlash(dir) + `"] :cli="target,t"
  ext:     "go"                         :cli="ext"
  tests:   true                         :cli="tests"
}

fs.walk :dirs=@config.targets :ext=@config.ext -> fs.filter :tests=@config.tests -> out.stdout
`

	pre, err := flow.PreprocessBytes([]byte(src), "app.nflow")
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}

	resolvedCfg := flow.BuildResolvedConfig(pre, nil)
	reg := flow.NewRegistry()
	if err := reg.Register(fsio.Library()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	prev := flow.DefaultRegistry()
	flow.SetDefaultRegistry(reg)
	t.Cleanup(func() { flow.SetDefaultRegistry(prev) })

	bld, err := flow.CompilePipeline(
		pre.DSL,
		action.MustNewRegistry(),
		flow.WithFlowRegistry(reg),
		flow.WithConfig(resolvedCfg),
	)
	if err != nil {
		t.Fatalf("CompilePipeline: %v", err)
	}

	out, err := bld.Build().DoAny(context.Background(), nil)
	if err != nil {
		t.Fatalf("DoAny: %v", err)
	}

	dr, ok := out.(flow.StreamDrainResult)
	if !ok {
		t.Fatalf("expected flow.StreamDrainResult, got %T", out)
	}

	// Expect exactly 1 file: main.go.
	// README.md is rejected by ext="go".
	// main_test.go is rejected by tests=true on the filter.
	if dr.Count != 1 {
		t.Fatalf("expected Count=1 (main.go), got %d", dr.Count)
	}
}

func TestResolveParams_CLIOverridesConfig(t *testing.T) {
	src := `@config {
  ext: "go" :cli="ext"
}
noop
`
	pre, _ := flow.PreprocessBytes([]byte(src), "test.nflow")

	cfg1 := flow.BuildResolvedConfig(pre, nil)
	if cfg1["ext"] != "go" {
		t.Fatalf("expected default 'go', got %q", cfg1["ext"])
	}

	cfg2 := flow.BuildResolvedConfig(pre, []string{"--ext=ts"})
	if cfg2["ext"] != "ts" {
		t.Fatalf("expected CLI override 'ts', got %q", cfg2["ext"])
	}
}

func TestResolveParams_EnvFallback(t *testing.T) {
	t.Setenv("NEXSS_EXT", "rust")

	src := `@config {
  ext: "go"
}
noop
`
	pre, _ := flow.PreprocessBytes([]byte(src), "test.nflow")
	cfg := flow.BuildResolvedConfig(pre, nil)

	if cfg["ext"] != "rust" {
		t.Fatalf("expected ENV override 'rust', got %q", cfg["ext"])
	}
}
