package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
)

type resolverWithoutCatalog struct {
	core.CapabilityResolver
}

func TestBuildConfig_LocalNamespaceQualifiersAreCompilationScoped(t *testing.T) {
	t.Parallel()
	canonical := action.New("transportnats.request", func(_ context.Context, in map[string]any) (map[string]any, error) {
		return in, nil
	}).Build()
	library := action.Library{Name: "transportnats", Actions: []action.AnyAction{canonical}}
	makeConfig := func(bundleID, qualifier string) Config {
		t.Helper()
		bundles := append(native.Bundles(), core.Bundle{
			ID:        bundleID,
			Alias:     qualifier,
			Libraries: []action.Library{library},
		})
		cfg, err := BuildConfig(bundles)
		if err != nil {
			t.Fatalf("BuildConfig(%s): %v", qualifier, err)
		}
		return cfg
	}

	natsConfig := makeConfig("local-nats", "nats")
	mqConfig := makeConfig("local-mq", "mq")
	if _, ok := natsConfig.Resolver.Action("mq.request"); ok {
		t.Fatal("nats compilation leaked the mq qualifier")
	}
	if _, ok := mqConfig.Resolver.Action("nats.request"); ok {
		t.Fatal("mq compilation leaked the nats qualifier")
	}
	if got := canonical.Describe().Name; got != "transportnats.request" {
		t.Fatalf("local mounting mutated caller action name: %q", got)
	}

	type runSpec struct {
		cfg     Config
		name    string
		file    string
		payload string
	}
	specs := []runSpec{
		{cfg: natsConfig, name: "nats.request", file: "nats.nflow", payload: "nats"},
		{cfg: mqConfig, name: "mq.request", file: "mq.nflow", payload: "mq"},
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(specs))
	for _, spec := range specs {
		wg.Go(func() {
			for i := range 20 {
				dsl := fmt.Sprintf(`%s @{ value: %q }`, spec.name, spec.payload)
				out, err := Execute(context.Background(), spec.cfg, dsl, spec.file, nil)
				if err != nil {
					errCh <- fmt.Errorf("%s run %d: %w", spec.file, i, err)
					return
				}
				result, ok := out.Output.(map[string]any)
				if !ok || result["value"] != spec.payload {
					errCh <- fmt.Errorf("%s run %d returned %#v", spec.file, i, out.Output)
					return
				}
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestBuildConfig_RejectsDuplicateLocalNamespaceQualifiers(t *testing.T) {
	t.Parallel()
	_, err := BuildConfig([]core.Bundle{
		{ID: "first", Alias: "shared", Libraries: []action.Library{{Name: "first"}}},
		{ID: "second", Alias: "shared", Libraries: []action.Library{{Name: "second"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate @require namespace qualifier "shared"`) {
		t.Fatalf("expected duplicate qualifier error, got %v", err)
	}
}

func TestExecutionResolver_MountRejectsCollisionsWithoutPartialCommit(t *testing.T) {
	t.Parallel()
	baseAction := action.New("base.shared", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	base, err := core.NewDynamicResolver(action.Library{Name: "base", Actions: []action.AnyAction{baseAction}})
	if err != nil {
		t.Fatal(err)
	}
	hook := action.AnyHook{Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
		return ctx, nil
	}}
	wrapped, err := newExecutionResolver(resolverWithoutCatalog{CapabilityResolver: base}, []action.AnyHook{hook})
	if err != nil {
		t.Fatal(err)
	}
	resolver, ok := wrapped.(*executionResolver)
	if !ok {
		t.Fatalf("newExecutionResolver returned %T, want *executionResolver", wrapped)
	}
	lib := action.Library{
		Name: "custom",
		Actions: []action.AnyAction{
			action.New("custom.first", func(_ context.Context, in any) (any, error) { return in, nil }).Build(),
			action.New("base.shared", func(_ context.Context, in any) (any, error) { return in, nil }).Build(),
		},
	}
	if err := resolver.Mount(lib); err == nil || !strings.Contains(err.Error(), "canonical collision") {
		t.Fatalf("expected collision error, got %v", err)
	}
	if _, ok := resolver.Action("custom.first"); ok {
		t.Fatal("invalid library partially committed its first action")
	}
	if act, ok := resolver.Action("base.shared"); !ok || act.Describe().Name != "base.shared" {
		t.Fatal("base resolver action disappeared after rejected mount")
	}
}
