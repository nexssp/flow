package nodes_dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/contracts"
)

type dispatchTestResolver map[string]action.AnyAction

func (r dispatchTestResolver) Action(name string) (action.AnyAction, bool) {
	target, ok := r[name]
	return target, ok
}

type orderedDispatchTestResolver struct {
	actions map[string]action.AnyAction
	order   *[]string
}

func (r *orderedDispatchTestResolver) Action(name string) (action.AnyAction, bool) {
	*r.order = append(*r.order, "lookup:"+name)
	target, ok := r.actions[name]
	return target, ok
}

func TestDispatchEmptyMembersReturnsNotFound(t *testing.T) {
	ctx := contracts.WithActionResolver(t.Context(), dispatchTestResolver{})
	_, err := Dispatch.Do(ctx, DispatchReq{})
	if got := xerr.KindFrom(err); got != xerr.KindNotFound {
		t.Fatalf("error kind = %q, want %q (error: %v)", got, xerr.KindNotFound, err)
	}
	if got, want := err.Error(), "action dispatch.run execution failed: [NotFound] dispatch: no member succeeded"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestDispatchReturnsLastCandidateError(t *testing.T) {
	firstErr := errors.New("first candidate failed")
	lastErr := errors.New("last candidate failed")
	var calls []string
	resolver := dispatchTestResolver{
		"test.first": action.New("test.first", func(context.Context, any) (any, error) {
			calls = append(calls, "first")
			return nil, firstErr
		}).Build(),
		"test.last": action.New("test.last", func(context.Context, any) (any, error) {
			calls = append(calls, "last")
			return nil, lastErr
		}).Build(),
	}
	ctx := contracts.WithActionResolver(t.Context(), resolver)
	_, err := Dispatch.Do(ctx, DispatchReq{Members: []string{"test.first", "missing", "test.last"}})
	if !errors.Is(err, lastErr) {
		t.Fatalf("error = %v, want last candidate error %v", err, lastErr)
	}
	if got, want := err.Error(), "action dispatch.run execution failed: action test.last execution failed: last candidate failed"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "last" {
		t.Fatalf("candidate calls = %v, want [first last]", calls)
	}
}

func TestDispatchReturnsLastMissingMemberNotFound(t *testing.T) {
	failure := errors.New("registered candidate failed")
	resolver := dispatchTestResolver{
		"test.fail": action.New("test.fail", func(context.Context, any) (any, error) {
			return nil, failure
		}).Build(),
	}
	ctx := contracts.WithActionResolver(t.Context(), resolver)
	_, err := Dispatch.Do(ctx, DispatchReq{Members: []string{"test.fail", "missing"}})
	if got := xerr.KindFrom(err); got != xerr.KindNotFound {
		t.Fatalf("error kind = %q, want %q (error: %v)", got, xerr.KindNotFound, err)
	}
	if got, want := err.Error(), "action dispatch.run execution failed: [NotFound] dispatch: member not registered: missing"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestDispatchLooksUpCandidatesLazilyAndStopsAtFirstSuccess(t *testing.T) {
	failure := errors.New("candidate failed")
	var order []string
	resolver := &orderedDispatchTestResolver{
		order: &order,
		actions: map[string]action.AnyAction{
			"test.fail": action.New("test.fail", func(context.Context, any) (any, error) {
				order = append(order, "run:test.fail")
				return nil, failure
			}).Build(),
			"test.ok": action.New("test.ok", func(context.Context, any) (any, error) {
				order = append(order, "run:test.ok")
				return "success", nil
			}).Build(),
			"test.late": action.New("test.late", func(context.Context, any) (any, error) {
				order = append(order, "run:test.late")
				return "unexpected", nil
			}).Build(),
		},
	}
	ctx := contracts.WithActionResolver(t.Context(), resolver)
	got, err := Dispatch.Do(ctx, DispatchReq{Members: []string{"test.fail", "test.ok", "test.late"}})
	if err != nil {
		t.Fatalf("Dispatch.Do returned error: %v", err)
	}
	if got != "success" {
		t.Fatalf("result = %#v, want %q", got, "success")
	}
	wantOrder := []string{"lookup:test.fail", "run:test.fail", "lookup:test.ok", "run:test.ok"}
	if len(order) != len(wantOrder) {
		t.Fatalf("lookup/action order = %v, want %v", order, wantOrder)
	}
	for i, want := range wantOrder {
		if order[i] != want {
			t.Fatalf("lookup/action order = %v, want %v", order, wantOrder)
		}
	}
}

func TestDispatchContinuesCandidatesWithCanceledContext(t *testing.T) {
	firstErr := errors.New("first candidate failed")
	lastErr := errors.New("last candidate failed")
	var calls int
	makeTarget := func(err error) action.AnyAction {
		return action.New("test.candidate", func(ctx context.Context, _ any) (any, error) {
			calls++
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Errorf("candidate context error = %v, want context.Canceled", ctx.Err())
			}
			return nil, err
		}).Build()
	}
	resolver := dispatchTestResolver{
		"test.first": makeTarget(firstErr),
		"test.last":  makeTarget(lastErr),
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ctx = contracts.WithActionResolver(ctx, resolver)
	_, err := Dispatch.Do(ctx, DispatchReq{Members: []string{"test.first", "test.last"}})
	if !errors.Is(err, lastErr) {
		t.Fatalf("error = %v, want last candidate error %v", err, lastErr)
	}
	if got, want := err.Error(), "action dispatch.run execution failed: action test.candidate execution failed: last candidate failed"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if calls != 2 {
		t.Fatalf("candidate calls = %d, want both candidates", calls)
	}
}

func TestDispatchContinuesFallbackWhenCandidateCancelsContext(t *testing.T) {
	firstErr := errors.New("first candidate failed")
	lastErr := errors.New("last candidate failed")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls int
	resolver := dispatchTestResolver{
		"test.first": action.New("test.first", func(context.Context, any) (any, error) {
			calls++
			cancel()
			return nil, firstErr
		}).Build(),
		"test.last": action.New("test.last", func(ctx context.Context, _ any) (any, error) {
			calls++
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Errorf("second candidate context error = %v, want context.Canceled", ctx.Err())
			}
			return nil, lastErr
		}).Build(),
	}
	ctx = contracts.WithActionResolver(ctx, resolver)
	_, err := Dispatch.Do(ctx, DispatchReq{Members: []string{"test.first", "test.last"}})
	if !errors.Is(err, lastErr) {
		t.Fatalf("error = %v, want last candidate error %v", err, lastErr)
	}
	if calls != 2 {
		t.Fatalf("candidate calls = %d, want both candidates", calls)
	}
}

func TestBundle_DeclaresCapabilityReferenceListForMembers(t *testing.T) {
	t.Parallel()
	schema := Bundle(nil).ArgSchemas["dispatch.run"]
	for _, field := range schema {
		if field.Name == "members" {
			ktest.RequireEqual(t, field.Kind, compiler.ArgCapabilityRefList)
			ktest.RequireCondition(t, !field.Optional, "dispatch.run.members must be required")
			return
		}
	}
	t.Fatal("dispatch.run schema has no members field")
}

func TestBundle_WiresDispatchAndFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Libraries[0].Actions), 1)
	ktest.RequireEqual(t, b.Libraries[0].Actions[0].Describe().Name, "dispatch.run")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}
