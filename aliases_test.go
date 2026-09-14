package flow_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func TestAliases_RegisterAndSkipCollisions(t *testing.T) {
	reg := flow.NewRegistry()

	canonical := action.New("read",
		func(_ context.Context, _ struct{}) (string, error) { return "", nil },
	).Build()
	reg.Register("read", canonical)

	reader := action.New("workspace.read_file",
		func(_ context.Context, _ struct{}) (string, error) { return "", nil },
	).Build()
	reg.Register("workspace.read_file", reader)

	flow.RegisterAliases(reg)

	got, _ := reg.Get("read")
	if got != canonical {
		t.Fatal("alias must not shadow a canonical name")
	}

	if _, ok := reg.Get("open"); !ok {
		t.Fatal("expected 'open' alias")
	}

	if _, ok := reg.Get("read_file"); !ok {
		t.Fatal("expected 'read_file' alias")
	}
}

func TestAliases_Idempotent(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("workspace.read_file", action.New("workspace.read_file",
		func(_ context.Context, _ struct{}) (string, error) { return "", nil },
	).Build())

	flow.RegisterAliases(reg)
	flow.RegisterAliases(reg)

	if _, ok := reg.Get("read"); !ok {
		t.Fatal("alias missing after double registration")
	}
}

func TestAliases_NilRegistryIsNoop(t *testing.T) {
	flow.RegisterAliases(nil)
}
