package core

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestDynamicResolver_MountIsAtomicOnInvalidLibrary(t *testing.T) {
	t.Parallel()
	first := action.New("demo.first", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	unnamed := action.New("second", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatal(err)
	}
	err = resolver.Mount(action.Library{Name: "invalid", Actions: []action.AnyAction{first, unnamed}})
	if err == nil || !strings.Contains(err.Error(), "fully-qualified") {
		t.Fatalf("expected unqualified canonical-name error, got %v", err)
	}
	if _, ok := resolver.Action("demo.first"); ok {
		t.Fatal("invalid library partially mounted its first action")
	}
}

func TestDynamicResolver_RejectsKernelActionAliasesAtomically(t *testing.T) {
	t.Parallel()
	act := action.New("legacy.run", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatal(err)
	}
	err = resolver.Mount(action.Library{
		Name:    "legacy",
		Actions: []action.AnyAction{act},
		Aliases: []action.Alias{{Canonical: "legacy.run", Short: []string{"run"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "Kernel action aliases") {
		t.Fatalf("expected Flow alias-policy error, got %v", err)
	}
	if _, ok := resolver.Action("legacy.run"); ok {
		t.Fatal("library with an extension alias partially mounted")
	}
}

func TestDynamicResolver_RejectsDuplicateMount(t *testing.T) {
	t.Parallel()
	act := action.New("demo.run", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	lib := action.Library{Name: "demo", Actions: []action.AnyAction{act}}
	resolver, err := NewDynamicResolver(lib)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.Mount(lib); err == nil || !strings.Contains(err.Error(), "duplicate mount") {
		t.Fatalf("expected duplicate-mount error, got %v", err)
	}
	if len(resolver.ActionNames()) != 1 {
		t.Fatalf("duplicate mount changed resolver: %v", resolver.ActionNames())
	}
}

func TestDynamicResolver_RejectsCanonicalVsLocalQualifierCollisionAtomically(t *testing.T) {
	t.Parallel()
	base := action.Library{
		Name:    "existing",
		Actions: []action.AnyAction{action.New("nats.request", func(_ context.Context, in any) (any, error) { return in, nil }).Build()},
	}
	resolver, err := NewDynamicResolver(base)
	if err != nil {
		t.Fatal(err)
	}
	transport := action.Library{
		Name:    "transportnats",
		Actions: []action.AnyAction{action.New("transportnats.request", func(_ context.Context, in any) (any, error) { return in, nil }).Build()},
	}
	err = resolver.MountWithAlias(transport, "nats")
	if err == nil || !strings.Contains(err.Error(), `name "nats.request"`) || !strings.Contains(err.Error(), "existing") || !strings.Contains(err.Error(), "transportnats as nats") {
		t.Fatalf("expected owner-rich canonical/qualifier conflict, got %v", err)
	}
	if _, ok := resolver.Action("transportnats.request"); ok {
		t.Fatal("conflicting qualified library was partially mounted")
	}
}

func TestDynamicResolver_QualifierCannotRescueUnqualifiedDeclaration(t *testing.T) {
	t.Parallel()
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatal(err)
	}
	lib := action.Library{
		Name:    "demo",
		Actions: []action.AnyAction{action.New("run", func(_ context.Context, in any) (any, error) { return in, nil }).Build()},
	}
	if err := resolver.MountWithAlias(lib, "local"); err == nil || !strings.Contains(err.Error(), "canonical fully-qualified name") {
		t.Fatalf("expected declaration error, got %v", err)
	}
	if _, ok := resolver.Action("local.run"); ok {
		t.Fatal("invalid unqualified declaration was mounted")
	}
}

func TestDynamicResolver_RejectsCrossKindCanonicalCollisions(t *testing.T) {
	t.Parallel()
	newAction := func(name string) action.AnyAction {
		return action.New(name, func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	}
	newOperator := func(name string) action.NamedOperator {
		return action.NamedOperator{
			Name: name, InType: reflect.TypeFor[any](), OutType: reflect.TypeFor[any](),
			ConfigType: reflect.TypeFor[struct{}](),
			Build:      func(any) (action.StreamOperator, error) { return testResolverOperator{name: name}, nil },
		}
	}
	tests := []struct {
		name   string
		first  action.Library
		second action.Library
		kinds  []string
		owners []string
	}{
		{
			name:   "action and source",
			first:  action.Library{Name: "action_owner", Actions: []action.AnyAction{newAction("shared.name")}},
			second: action.Library{Name: "source_owner", Sources: []action.AnyStreamAction{fakeStreamSource("shared.name")}},
			kinds:  []string{"action", "source"}, owners: []string{"action_owner", "source_owner"},
		},
		{
			name:   "action and operator",
			first:  action.Library{Name: "action_owner", Actions: []action.AnyAction{newAction("shared.name")}},
			second: action.Library{Name: "operator_owner", Operators: []action.NamedOperator{newOperator("shared.name")}},
			kinds:  []string{"action", "operator"}, owners: []string{"action_owner", "operator_owner"},
		},
		{
			name:   "source and operator",
			first:  action.Library{Name: "source_owner", Sources: []action.AnyStreamAction{fakeStreamSource("shared.name")}},
			second: action.Library{Name: "operator_owner", Operators: []action.NamedOperator{newOperator("shared.name")}},
			kinds:  []string{"source", "operator"}, owners: []string{"source_owner", "operator_owner"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resolver, err := NewDynamicResolver(test.first)
			if err != nil {
				t.Fatal(err)
			}
			if err := resolver.Mount(test.second); err == nil {
				t.Fatal("expected cross-kind canonical collision")
			} else {
				for _, part := range append(append([]string{`name "shared.name"`}, test.kinds...), test.owners...) {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("error %q does not contain %q", err, part)
					}
				}
			}
			if _, ok := resolver.Action("shared.name"); test.first.Actions != nil && !ok {
				t.Fatal("first action disappeared after rejected mount")
			}
			if src, ok := resolver.Stream("shared.name"); test.second.Sources != nil && ok {
				t.Fatalf("colliding source was partially mounted: %v", src)
			}
			if op, ok := resolver.Operator("shared.name"); test.second.Operators != nil && ok {
				t.Fatalf("colliding operator was partially mounted: %v", op.Name)
			}
		})
	}
}

func TestDynamicResolver_MountWithAliasReplacesNamespacesForAllKinds(t *testing.T) {
	t.Parallel()
	act := action.New("demo.run", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	op := action.NamedOperator{
		Name: "demo.filter", InType: reflect.TypeFor[any](), OutType: reflect.TypeFor[any](),
		ConfigType: reflect.TypeFor[struct{}](),
		Build:      func(any) (action.StreamOperator, error) { return testResolverOperator{name: "demo.filter"}, nil },
	}
	lib := action.Library{
		Name:      "demo",
		Actions:   []action.AnyAction{act},
		Sources:   []action.AnyStreamAction{fakeStreamSource("demo.items", 1)},
		Operators: []action.NamedOperator{op},
	}
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.MountWithAlias(lib, "local"); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolver.Action("local.run"); !ok {
		t.Fatal("qualified action missing")
	}
	if _, ok := resolver.Action("local.demo.run"); ok {
		t.Fatal("namespace was prefixed instead of replaced")
	}
	source, ok := resolver.Stream("local.items")
	if !ok || source.Describe().Name != "local.items" {
		t.Fatal("qualified stream source missing or incorrectly named")
	}
	mountedOp, ok := resolver.Operator("local.filter")
	if !ok || mountedOp.Name != "local.filter" {
		t.Fatal("qualified operator missing or incorrectly named")
	}
	built, err := mountedOp.Build(nil)
	if err != nil || built.Name() != "local.filter" {
		t.Fatalf("built operator name = %v, err = %v", built, err)
	}
	if got := act.Describe().Name; got != "demo.run" {
		t.Fatalf("MountWithAlias mutated caller action: %q", got)
	}
}

type testResolverOperator struct{ name string }

func (o testResolverOperator) Name() string { return o.name }
func (o testResolverOperator) Apply(up action.AnyStream) (action.AnyStream, error) {
	return up, nil
}
