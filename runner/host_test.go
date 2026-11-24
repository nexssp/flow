package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
