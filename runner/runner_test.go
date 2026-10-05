package runner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/extensions/selftestkit"
	"github.com/nexssp/flow/native"
)

func TestExecute_DispatchSeesHooks(t *testing.T) {
	bundles := append(native.Bundles(), selftestkit.Bundle(nil))
	cfg, err := BuildConfig(bundles)
	if err != nil {
		t.Fatal(err)
	}

	var fired atomic.Int32
	cfg.Hooks = []action.AnyHook{{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			fired.Add(1)
			return ctx, nil
		},
	}}

	_, err = Execute(
		context.Background(),
		cfg,
		`dispatch.run @{ members: [cov.echo], payload: { value: 42 } }`,
		"dispatch_test.nflow",
		nil,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fired.Load() == 0 {
		t.Fatal("hooks never fired — registry probably nil")
	}
}

func TestCompileAndExecute_DispatchCapabilityRefListFallback(t *testing.T) {
	cfg, err := BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	src := `dispatch.run @{ members: [runtime.fail, runtime.const], payload: { value: "I am the fallback!" } }`

	compiled, err := Compile(context.Background(), cfg, src, "dispatch_test.nflow")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.Program == nil {
		t.Fatal("Compile returned no program")
	}

	ex, err := Execute(context.Background(), cfg, src, "dispatch_test.nflow", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := ex.Output, "I am the fallback!"; got != want {
		t.Fatalf("Execute output = %#v, want %q", got, want)
	}
}

func TestCompile_MaterializerMountsAreExecutionScoped(t *testing.T) {
	cfg, err := BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	src := `@pool workers [runtime.const]
pool.workers @{ value: "p" }`

	for i := range 2 {
		if _, err := Compile(context.Background(), cfg, src, "pool_materializer.nflow"); err != nil {
			t.Fatalf("Compile run %d: %v", i+1, err)
		}
	}
	if _, ok := cfg.Resolver.Action("pool.workers"); ok {
		t.Fatal("execution-scoped pool action leaked into the base resolver")
	}
}
