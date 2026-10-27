package compiler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunScopeCancelsThenCleansUpInReverseOrder(t *testing.T) {
	t.Parallel()
	runCtx, scope := NewRunScope(context.Background())
	var order []int
	for _, value := range []int{1, 2} {
		_, err := scope.Register(func(cleanupCtx context.Context) error {
			if cleanupCtx.Err() != nil {
				t.Errorf("cleanup context is canceled: %v", cleanupCtx.Err())
			}
			order = append(order, value)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := scope.Close(runCtx); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(runCtx.Err(), context.Canceled) {
		t.Fatalf("run context error = %v, want canceled", runCtx.Err())
	}
	if !reflect.DeepEqual(order, []int{2, 1}) {
		t.Fatalf("cleanup order = %v, want [2 1]", order)
	}
	if err := scope.Close(runCtx); err != nil {
		t.Fatalf("second close = %v, want nil", err)
	}
	if _, err := scope.Register(func(context.Context) error { return nil }); !errors.Is(err, ErrRunScopeClosed) {
		t.Fatalf("register after close error = %v, want ErrRunScopeClosed", err)
	}
}

func TestRunScopeReleaseSkipsCompletedResource(t *testing.T) {
	t.Parallel()
	_, scope := NewRunScope(context.Background())
	called := false
	release, err := scope.Register(func(context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	if err := scope.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("released cleanup callback was invoked")
	}
}

func TestRunScopeBoundsCleanupAndJoinsErrorsAfterPanics(t *testing.T) {
	t.Parallel()
	_, scope := NewRunScope(context.Background())
	cleanupErr := errors.New("cleanup failed")
	called := 0
	for _, cleanup := range []RunCleanupFunc{
		func(ctx context.Context) error {
			called++
			if ctx.Err() != nil {
				t.Errorf("cleanup context is canceled: %v", ctx.Err())
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("cleanup context has no deadline")
			} else if remaining := time.Until(deadline); remaining > runCleanupTimeout || remaining < runCleanupTimeout-time.Second {
				t.Errorf("cleanup deadline has %s remaining, want close to %s", remaining, runCleanupTimeout)
			}
			return cleanupErr
		},
		func(context.Context) error {
			called++
			panic("cleanup panic")
		},
		func(context.Context) error {
			called++
			return nil
		},
	} {
		if _, err := scope.Register(cleanup); err != nil {
			t.Fatal(err)
		}
	}

	closeCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := scope.Close(closeCtx)
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("close error = %v, want joined cleanup error", err)
	}
	if !strings.Contains(err.Error(), "cleanup callback panicked: cleanup panic") {
		t.Fatalf("close error = %v, want recovered panic", err)
	}
	if called != 3 {
		t.Fatalf("cleanup callbacks called = %d, want 3", called)
	}
}
