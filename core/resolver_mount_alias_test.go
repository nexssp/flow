package core

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestDynamicResolverMountWithAliasReplacesNamespaceWithoutMutatingLibrary(t *testing.T) {
	greet := action.New("demo.greet", func(context.Context, string) (string, error) {
		return "ok", nil
	}).Build()
	lib := action.Library{
		Name:    "demo",
		Actions: []action.AnyAction{greet},
	}

	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("NewDynamicResolver() error = %v", err)
	}
	if err := resolver.MountWithAlias(lib, "remote"); err != nil {
		t.Fatalf("MountWithAlias() error = %v", err)
	}

	if lib.Name != "demo" {
		t.Fatalf("MountWithAlias mutated library name: got %q", lib.Name)
	}
	if got := lib.Actions[0].Describe().Name; got != "demo.greet" {
		t.Fatalf("MountWithAlias mutated action name: got %q", got)
	}

	if _, ok := resolver.Action("remote.greet"); !ok {
		t.Fatal("aliased canonical action was not mounted")
	}
	if _, ok := resolver.Action("remote.demo.greet"); ok {
		t.Fatal("namespace qualifier was prefixed instead of replacing the canonical namespace")
	}

	second, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("second NewDynamicResolver() error = %v", err)
	}
	if err := second.Mount(lib); err != nil {
		t.Fatalf("Mount(original library) error = %v", err)
	}
	if _, ok := second.Action("demo.greet"); !ok {
		t.Fatal("original library could not be mounted after MountWithAlias")
	}
}
