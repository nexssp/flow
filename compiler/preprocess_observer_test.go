package compiler

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

type observerLoader struct {
	calls int
}

func (l *observerLoader) Load(_, target string) (data []byte, resolved string, err error) {
	l.calls++
	return []byte("child.action"), target, nil
}

func TestPreprocessObserverUsesExistingLoaderAndLifecycle(t *testing.T) {
	t.Parallel()
	loader := &observerLoader{}
	var events []PreprocessEvent
	var previous []string
	ctx := WithSourceLoader(t.Context(), loader)
	ctx = WithPreprocessObserver(ctx, func(event PreprocessEvent) { previous = append(previous, event.File) })
	ctx = WithPreprocessObserver(ctx, func(event PreprocessEvent) { events = append(events, event) })
	dt := NewDirectiveTable(Directive{Name: "child", Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
		clean, _, err := req.Recurse("virtual-child")
		req.Body[req.I] = clean
		return DirectiveRes{Next: req.I + 1}, err
	}})
	clean, _, err := Preprocess(ctx, dt, "@child\nroot.action", "virtual-root")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, clean, "child.action\nroot.action")
	ktest.RequireEqual(t, loader.calls, 1)
	ktest.RequireEqual(t, len(events), 2)
	ktest.RequireEqual(t, previous, []string{"virtual-child", "virtual-root"})
	ktest.RequireEqual(t, events[0].Source, "child.action")
	ktest.RequireEqual(t, events[1].Directives, []string{"child"})
}

func TestPreprocessObserverRetainsExpandedDeclarationBodies(t *testing.T) {
	t.Parallel()
	var event PreprocessEvent
	dt := NewDirectiveTable(Directive{Name: "substitute", Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
		req.Lines[1] = "selected.action"
		return DirectiveRes{Next: 2}, nil
	}})
	ctx := WithPreprocessObserver(t.Context(), func(value PreprocessEvent) { event = value })
	_, _, err := Preprocess(ctx, dt, "@substitute\n${ACTION}", "root")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, event.Original, "@substitute\n${ACTION}")
	ktest.RequireEqual(t, event.Source, "@substitute\nselected.action")
	ktest.RequireEqual(t, event.Clean, "\n")
}

func TestPreprocessObserverCancelledDoesNotReportSuccess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	ctx = WithPreprocessObserver(ctx, func(PreprocessEvent) { called = true })
	_, _, err := Preprocess(ctx, nil, "runtime.noop", "root")
	ktest.RequireErrorIs(t, err, context.Canceled)
	ktest.RequireFalse(t, called)
}

func TestPreprocessObserverNilTableAndNilObserver(t *testing.T) {
	t.Parallel()
	var events []PreprocessEvent
	ctx := WithPreprocessObserver(t.Context(), func(value PreprocessEvent) { events = append(events, value) })
	ctx = WithPreprocessObserver(ctx, nil)
	clean, _, err := Preprocess(ctx, nil, "runtime.noop", "root")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, clean, "runtime.noop")
	ktest.RequireEqual(t, len(events), 1)
}
