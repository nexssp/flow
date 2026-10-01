package core

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestComposedPipe_DelegatesSequentialExecution(t *testing.T) {
	left := action.New("left", func(_ context.Context, in int) (int, error) {
		return in + 1, nil
	}).Build()
	right := action.New("right", func(_ context.Context, in int) (int, error) {
		return in * 2, nil
	}).Build()

	pipe := ComposedPipe(left, right)
	got, err := pipe.DoAny(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Fatalf("output = %#v, want 42", got)
	}
	if pipe.Describe().Name != "pipe" {
		t.Fatalf("name = %q, want pipe", pipe.Describe().Name)
	}
}

func TestComposedPipe_PropagatesChildErrorUnchanged(t *testing.T) {
	wantErr := errors.New("left failed")
	left := action.New("left", func(context.Context, any) (any, error) {
		return nil, wantErr
	}).Build()
	right := action.New("right", func(context.Context, any) (any, error) {
		t.Fatal("right action must not execute")
		return nil, nil
	}).Build()

	_, err := ComposedPipe(left, right).DoAny(context.Background(), nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want left error %v", err, wantErr)
	}
}

func TestComposedPipe_PropagatesCancellationToChild(t *testing.T) {
	left := action.New("left", func(ctx context.Context, _ any) (any, error) {
		return nil, ctx.Err()
	}).Build()
	right := action.New("right", func(context.Context, any) (any, error) {
		t.Fatal("right action must not execute")
		return nil, nil
	}).Build()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ComposedPipe(left, right).DoAny(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestComposedPipe_PreservesChildBindings(t *testing.T) {
	left := action.New("left", func(context.Context, any) (any, error) {
		return nil, nil
	}).Route("left-binding").Build()
	right := action.New("right", func(context.Context, any) (any, error) {
		return nil, nil
	}).Route("right-binding").Build()

	bindings := ComposedPipe(left, right).GetBindings()
	if len(bindings) != 2 {
		t.Fatalf("got %d bindings, want 2", len(bindings))
	}
	if bindings[0] != "left-binding" || bindings[1] != "right-binding" {
		t.Fatalf("bindings = %#v, want left/right bindings", bindings)
	}
}
