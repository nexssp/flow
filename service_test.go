package flow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/dslparse"
	"github.com/nexssp/kernel/action"
)

// fakeResolver records whether it was asked to match, and what it was
// asked to resolve. It never modifies the action.
//
// The resolver is registered per-test after a ResetResolvers call, so
// the state written by Match and Resolve is never shared with another
// test running in parallel.
type fakeResolver struct {
	shouldHit bool

	matched     bool
	resolved    bool
	lastActName string
	lastMods    dslparse.Modifiers
}

func (f *fakeResolver) Match(_ string, mods dslparse.Modifiers) bool {
	f.matched = true
	f.lastMods = mods
	return f.shouldHit
}

func (f *fakeResolver) Resolve(
	_ string,
	_ dslparse.Modifiers,
	act action.AnyAction,
) (action.AnyAction, error) {
	f.resolved = true
	f.lastActName = act.Describe().Name
	return act, nil
}

func TestServiceResolver_NotCalledWhenNoMatch(t *testing.T) {
	flow.ResetResolvers()
	t.Cleanup(func() { flow.ResetResolvers() })

	r := &fakeResolver{shouldHit: false}
	flow.RegisterResolver(r)

	reg := action.MustNewRegistry(action.Of(
		action.New("plain.action", func(_ context.Context, s string) (string, error) {
			return s, nil
		}).Build(),
	))

	_, err := flow.CompilePipeline(`plain.action:timeout=5s`, reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !r.matched {
		t.Error("resolver was not consulted")
	}
	if r.resolved {
		t.Error("resolver should not have resolved")
	}
}

func TestServiceResolver_CalledWhenMatch(t *testing.T) {
	flow.ResetResolvers()
	t.Cleanup(func() { flow.ResetResolvers() })

	r := &fakeResolver{shouldHit: true}
	flow.RegisterResolver(r)

	reg := action.MustNewRegistry(action.Of(
		action.New("domain.action", func(_ context.Context, s string) (string, error) {
			return s, nil
		}).Build(),
	))

	_, err := flow.CompilePipeline(`domain.action:provider="deepseek"`, reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !r.resolved {
		t.Fatal("resolver should have been called")
	}
	if r.lastActName != "domain.action" {
		t.Errorf("resolver saw %q", r.lastActName)
	}
	if r.lastMods.Provider != "deepseek" {
		t.Errorf("Provider=%q", r.lastMods.Provider)
	}
}

func TestServiceResolver_ErrorPropagates(t *testing.T) {
	flow.ResetResolvers()
	t.Cleanup(func() { flow.ResetResolvers() })

	flow.RegisterResolver(&failingResolver{})

	reg := action.MustNewRegistry(action.Of(
		action.New("bad.action", func(_ context.Context, s string) (string, error) {
			return s, nil
		}).Build(),
	))

	_, err := flow.CompilePipeline(`bad.action:provider="x"`, reg)
	if err == nil {
		t.Fatal("expected resolver error to propagate")
	}
}

type failingResolver struct{}

func (failingResolver) Match(_ string, _ dslparse.Modifiers) bool { return true }
func (failingResolver) Resolve(
	_ string,
	_ dslparse.Modifiers,
	_ action.AnyAction,
) (action.AnyAction, error) {
	return nil, errors.New("resolver refused")
}
