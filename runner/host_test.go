package runner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestHostRun_ShutsDownOnceInReverseOwnershipOrder(t *testing.T) {
	var got []string
	host := NewHost()
	bundles := []core.Bundle{
		{ID: "same", Shutdowns: []core.ShutdownFunc{
			func(context.Context) error { got = append(got, "first-a"); return nil },
			func(context.Context) error { got = append(got, "first-b"); return nil },
		}},
		{ID: "same", Shutdowns: []core.ShutdownFunc{
			func(context.Context) error { got = append(got, "second"); return nil },
		}},
	}
	ktest.RequireNoError(t, host.OwnAll(bundles))
	ktest.RequireNoError(t, host.Run(context.Background(), func(context.Context) error {
		got = append(got, "run")
		return nil
	}))
	ktest.RequireEqual(t, strings.Join(got, ","), "run,second,first-b,first-a")
	ktest.RequireNoError(t, host.Shutdown(context.Background()))
	ktest.RequireEqual(t, strings.Join(got, ","), "run,second,first-b,first-a")
}

func TestHostRun_JoinsPrimaryAndAllCleanupErrors(t *testing.T) {
	primary := errors.New("operation failed")
	cleanupA := errors.New("cleanup A")
	cleanupB := errors.New("cleanup B")
	var got []string
	host := NewHost()
	ktest.RequireNoError(t, host.OwnAll([]core.Bundle{
		{ID: "a", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			got = append(got, "a")
			return cleanupA
		}}},
		{ID: "b", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			got = append(got, "b")
			return cleanupB
		}}},
	}))

	err := host.Run(context.Background(), func(context.Context) error { return primary })
	ktest.RequireCondition(t, errors.Is(err, primary), "primary error must remain discoverable")
	ktest.RequireCondition(t, errors.Is(err, cleanupA), "first cleanup error must remain discoverable")
	ktest.RequireCondition(t, errors.Is(err, cleanupB), "second cleanup error must remain discoverable")
	ktest.RequireCondition(t, strings.Index(err.Error(), primary.Error()) < strings.Index(err.Error(), cleanupB.Error()), "primary error must format first")
	ktest.RequireEqual(t, strings.Join(got, ","), "b,a")
}

func TestHostRun_CanceledContextGetsFreshBoundedCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), testContextKey{}, "retained"))
	cancel()
	host := NewHost(WithShutdownTimeout(50 * time.Millisecond))
	cleanupDone := make(chan struct{})
	ktest.RequireNoError(t, host.Own(core.Bundle{ID: "cancel", Shutdowns: []core.ShutdownFunc{
		func(shutdownCtx context.Context) error {
			defer close(cleanupDone)
			if shutdownCtx.Err() != nil {
				return fmt.Errorf("cleanup started canceled: %w", shutdownCtx.Err())
			}
			if got := shutdownCtx.Value(testContextKey{}); got != "retained" {
				return fmt.Errorf("context value not retained: %v", got)
			}
			deadline, ok := shutdownCtx.Deadline()
			if !ok || time.Until(deadline) <= 0 {
				return errors.New("cleanup deadline missing")
			}
			<-shutdownCtx.Done()
			return shutdownCtx.Err()
		},
	}}))

	started := time.Now()
	err := host.Run(ctx, func(runCtx context.Context) error { return runCtx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled primary/cleanup error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("shutdown exceeded configured bound: %s", elapsed)
	}
	select {
	case <-cleanupDone:
	default:
		t.Fatal("cleanup callback did not complete")
	}
}

type testContextKey struct{}

func TestHostRun_PreservesOriginalPanicAndRunsCleanup(t *testing.T) {
	panicValue := &struct{ message string }{"original panic"}
	cleanupErr := errors.New("cleanup failed")
	var got string
	host := NewHost()
	ktest.RequireNoError(t, host.Own(core.Bundle{ID: "panic", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
		got = "closed"
		return cleanupErr
	}}}))

	defer func() {
		recovered := recover()
		if recovered != panicValue {
			t.Fatalf("panic value changed: got %#v want %#v", recovered, panicValue)
		}
		if got != "closed" {
			t.Fatalf("cleanup did not run before panic escaped: %q", got)
		}
	}()
	_ = host.Run(context.Background(), func(context.Context) error { panic(panicValue) })
}

func TestHostShutdown_ConcurrentCallsAreOnceOnlyAndRejectLateOwnership(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	started := make(chan struct{})
	release := make(chan struct{})
	host := NewHost()
	ktest.RequireNoError(t, host.Own(core.Bundle{ID: "concurrent", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release
		return nil
	}}}))

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_ = host.Shutdown(context.Background())
		})
	}
	<-started
	if err := host.Own(core.Bundle{ID: "late", Shutdowns: []core.ShutdownFunc{func(context.Context) error { return nil }}}); !errors.Is(err, ErrHostShutdownStarted) {
		t.Fatalf("late adoption error = %v", err)
	}
	close(release)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	ktest.RequireEqual(t, calls, 1)
}

func TestHostCleanupErrorFromPanickingCallbackDoesNotStopLaterCleanup(t *testing.T) {
	var got []string
	host := NewHost()
	ktest.RequireNoError(t, host.OwnAll([]core.Bundle{
		{ID: "first", Shutdowns: []core.ShutdownFunc{func(context.Context) error { got = append(got, "first"); return nil }}},
		{ID: "panic", Shutdowns: []core.ShutdownFunc{func(context.Context) error { got = append(got, "panic"); panic("cleanup panic") }}},
		{ID: "last", Shutdowns: []core.ShutdownFunc{func(context.Context) error { got = append(got, "last"); return nil }}},
	}))
	err := host.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cleanup callback panicked: cleanup panic") {
		t.Fatalf("expected panic cleanup error, got %v", err)
	}
	if !reflect.DeepEqual(got, []string{"last", "panic", "first"}) {
		t.Fatalf("cleanup order %v", got)
	}
}

func TestHostShutdown_UsesOneBoundAndAttemptsRemainingCallbacks(t *testing.T) {
	var got []string
	host := NewHost(WithShutdownTimeout(25 * time.Millisecond))
	ktest.RequireNoError(t, host.OwnAll([]core.Bundle{
		{ID: "first", Shutdowns: []core.ShutdownFunc{func(ctx context.Context) error {
			got = append(got, "first")
			return ctx.Err()
		}}},
		{ID: "last", Shutdowns: []core.ShutdownFunc{func(ctx context.Context) error {
			got = append(got, "last")
			<-ctx.Done()
			return ctx.Err()
		}}},
	}))
	started := time.Now()
	err := host.Shutdown(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("shutdown exceeded its single shared bound: %s", elapsed)
	}
	if !reflect.DeepEqual(got, []string{"last", "first"}) {
		t.Fatalf("shutdown calls = %v, want last,first", got)
	}
}

func TestHostRun_RejectsConcurrentRunAndShutdownUntilCallbackReturns(t *testing.T) {
	var mu sync.Mutex
	closed := 0
	started := make(chan struct{})
	release := make(chan struct{})
	runDone := make(chan error, 1)
	host := NewHost()
	ktest.RequireNoError(t, host.Own(core.Bundle{ID: "active", Shutdowns: []core.ShutdownFunc{func(context.Context) error {
		mu.Lock()
		closed++
		mu.Unlock()
		return nil
	}}}))
	go func() {
		runDone <- host.Run(context.Background(), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	if err := host.Run(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrHostAlreadyRunning) {
		t.Fatalf("concurrent Run error = %v, want ErrHostAlreadyRunning", err)
	}
	if err := host.Shutdown(context.Background()); !errors.Is(err, ErrHostInvocationRunning) {
		t.Fatalf("early Shutdown error = %v, want ErrHostInvocationRunning", err)
	}
	mu.Lock()
	if closed != 0 {
		mu.Unlock()
		t.Fatal("bundle closed while invocation callback was running")
	}
	mu.Unlock()
	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("Run error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if closed != 1 {
		t.Fatalf("shutdown calls = %d, want 1", closed)
	}
}

func TestHostRun_ReusesConfigAcrossExecutionsWithoutPrematureClose(t *testing.T) {
	closed := false
	runs := 0
	act := action.New("hostreuse.run", func(context.Context, any) (any, error) {
		if closed {
			return nil, errors.New("bundle was closed before Execute returned")
		}
		runs++
		return "ok", nil
	}).Build()
	bundle := core.Bundle{
		ID:        "hostreuse",
		Libraries: []action.Library{{Name: "hostreuse", Actions: []action.AnyAction{act}}},
		Shutdowns: []core.ShutdownFunc{func(context.Context) error {
			closed = true
			return nil
		}},
	}
	host := NewHost()
	ktest.RequireNoError(t, host.Own(bundle))
	err := host.Run(context.Background(), func(ctx context.Context) error {
		bundles := append(native.Bundles(), bundle)
		cfg, err := BuildConfig(bundles)
		if err != nil {
			return err
		}
		for range 2 {
			if _, err := Execute(ctx, cfg, "hostreuse.run", "reused.nflow", nil); err != nil {
				return err
			}
			if closed {
				return errors.New("Execute closed the bundle before the outer host returned")
			}
		}
		return nil
	})
	ktest.RequireNoError(t, err)
	if runs != 2 || !closed {
		t.Fatalf("action runs=%d closed=%v; want 2,true", runs, closed)
	}
}
