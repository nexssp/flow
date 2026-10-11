package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

type resolverWithoutCatalog struct{ core.CapabilityResolver }

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
