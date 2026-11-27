package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostFinishRunClosesAdmission(t *testing.T) {
	host := NewHost()
	host.mu.Lock()
	host.running = true
	host.mu.Unlock()

	host.finishRun()

	called := false
	err := host.Run(t.Context(), func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrHostShutdownStarted) {
		t.Fatalf("Host.Run error = %v, want ErrHostShutdownStarted", err)
	}
	if called {
		t.Fatal("Host.Run callback ran after the host finished")
	}
}

func TestHostRunPropagatesCancellationAfterCallbackReturns(t *testing.T) {
	host := NewHost()
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	defer func() {
		select {
		case release <- struct{}{}:
		default:
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- host.Run(ctx, func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("host callback did not start")
	}
	cancel()
	release <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Host.Run error = %v, want context.Canceled after callback returned", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Host.Run did not finish after callback was released")
	}
}
