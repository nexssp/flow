package core

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestDynamicResolverMountWithAliasDoesNotMutateLibrary(t *testing.T) {
	greet := action.New("greet", func(context.Context, string) (string, error) {
		return "ok", nil
	}).Build()
	lib := action.Library{
		Name:    "demo",
		Actions: []action.AnyAction{greet},
		Aliases: []action.Alias{{Canonical: "greet", Short: []string{"hi"}}},
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
	if got := lib.Actions[0].Describe().Name; got != "greet" {
		t.Fatalf("MountWithAlias mutated action name: got %q", got)
	}
	if got := lib.Aliases[0].Canonical; got != "greet" {
		t.Fatalf("MountWithAlias mutated canonical alias: got %q", got)
	}
	if got := lib.Aliases[0].Short[0]; got != "hi" {
		t.Fatalf("MountWithAlias mutated short alias: got %q", got)
	}

	if _, ok := resolver.Action("remote.greet"); !ok {
		t.Fatal("aliased canonical action was not mounted")
	}
	if _, ok := resolver.Action("remote.hi"); !ok {
		t.Fatal("aliased short action was not mounted")
	}

	second, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("second NewDynamicResolver() error = %v", err)
	}
	if err := second.Mount(lib); err != nil {
		t.Fatalf("Mount(original library) error = %v", err)
	}
	if _, ok := second.Action("greet"); !ok {
		t.Fatal("original library could not be mounted after MountWithAlias")
	}
	if _, ok := second.Action("hi"); !ok {
		t.Fatal("original library alias could not be mounted after MountWithAlias")
	}
}
