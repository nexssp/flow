package cli

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/nexssp/flow/core"
)

func TestRunCLIWithSignals_CancellationWaitsForActionThenShutsDownOnce(t *testing.T) {
	for _, signal := range []struct {
		name   string
		signal os.Signal
	}{
		{name: "SIGINT", signal: os.Interrupt},
		{name: "SIGTERM", signal: syscall.SIGTERM},
	} {
		t.Run(signal.name, func(t *testing.T) {
			for _, test := range []struct {
				name                   string
				source                 string
				wantCancellationStatus bool
			}{
				{
					name:                   "direct action",
					source:                 "lifecycle_cli_signal.run\n",
					wantCancellationStatus: true,
				},
				{
					name:   "supervisor child",
					source: `supervisor.run @{ tasks: [{ id: "child", dsl: "lifecycle_cli_signal.run", payload: {} }] }`,
				},
			} {
				t.Run(test.name, func(t *testing.T) {
					started := make(chan struct{})
					cancellationObserved := make(chan struct{})
					releaseAction := make(chan struct{})
					var actionFinished atomic.Bool
					var shutdownCalls atomic.Int32

					bundle := lifecycleBundle("lifecycle_cli_signal", func(ctx context.Context) (any, error) {
						close(started)
						<-ctx.Done()
						close(cancellationObserved)
						<-releaseAction
						actionFinished.Store(true)
						if test.wantCancellationStatus {
							return nil, ctx.Err()
						}
						return "completed after cancellation", nil
					}, func(ctx context.Context) error {
						shutdownCalls.Add(1)
						if !actionFinished.Load() {
							return errors.New("shutdown began before the action completed")
						}
						if ctx.Err() != nil {
							return errors.New("shutdown received a canceled context")
						}
						return nil
					})
					inv, err := newInvocation([]core.Bundle{bundle}, nil)
					if err != nil {
						t.Fatal(err)
					}
					path := writeLifecycleFlow(t, test.source)

					var signalSink chan<- os.Signal
					var registered atomic.Bool
					var stopCalls atomic.Int32
					var defaultSignalCalls atomic.Int32
					unregistered := make(chan struct{})
					register := func(ch chan<- os.Signal, signals ...os.Signal) func() {
						wantSignals := commandSignals()
						if len(signals) != len(wantSignals) {
							t.Errorf("registered signals = %v; want %v", signals, wantSignals)
						}
						for i, got := range signals {
							if i < len(wantSignals) && got != wantSignals[i] {
								t.Errorf("registered signal %d = %v; want %v", i, got, wantSignals[i])
							}
						}
						signalSink = ch
						registered.Store(true)
						return func() {
							if registered.Swap(false) {
								stopCalls.Add(1)
								close(unregistered)
							}
						}
					}
					deliver := func(sig os.Signal) {
						if registered.Load() {
							signalSink <- sig
							return
						}
						defaultSignalCalls.Add(1)
					}

					done := make(chan int, 1)
					go func() {
						done <- runCLIWithSignals(context.Background(), inv, []string{"run", path, "--json"}, register)
					}()
					<-started
					deliver(signal.signal)
					<-unregistered
					<-cancellationObserved

					select {
					case code := <-done:
						t.Fatalf("CLI returned with code %d before the canceled action completed", code)
					default:
					}
					if got := shutdownCalls.Load(); got != 0 {
						t.Fatalf("shutdown calls before action completion = %d, want 0", got)
					}

					// Once the first signal unregisters the CLI, a repeated one follows
					// the injected source's default path rather than being swallowed.
					deliver(signal.signal)
					if got := defaultSignalCalls.Load(); got != 1 {
						t.Fatalf("repeated signal default deliveries = %d, want 1", got)
					}

					close(releaseAction)
					code := <-done
					if test.wantCancellationStatus && code == 0 {
						t.Fatal("canceled direct action returned success")
					}
					if got := shutdownCalls.Load(); got != 1 {
						t.Fatalf("shutdown calls = %d, want 1", got)
					}
					if got := stopCalls.Load(); got != 1 {
						t.Fatalf("signal registration stop calls = %d, want 1", got)
					}
				})
			}
		})
	}
}

func TestRunCLIWithSignals_NormalCompletionStopsRegistrationWithoutCancelingParent(t *testing.T) {
	parent := t.Context()
	inv, err := newInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var registerCalls atomic.Int32
	var stopCalls atomic.Int32
	var registered atomic.Bool
	register := func(_ chan<- os.Signal, signals ...os.Signal) func() {
		registerCalls.Add(1)
		if len(signals) != len(commandSignals()) {
			t.Errorf("registered signals = %v; want %v", signals, commandSignals())
		}
		registered.Store(true)
		return func() {
			if registered.Swap(false) {
				stopCalls.Add(1)
			}
		}
	}

	code := withSignalContext(parent, register, func(ctx context.Context) int {
		if ctx.Err() != nil {
			t.Errorf("invocation context was canceled before dispatch: %v", ctx.Err())
		}
		return runInvocation(ctx, inv, func(context.Context) int { return 0 })
	})
	if code != 0 {
		t.Fatalf("normal exit code = %d, want 0", code)
	}
	if registerCalls.Load() != 1 || stopCalls.Load() != 1 || registered.Load() {
		t.Fatalf("registrations=%d stops=%d active=%v; want 1,1,false", registerCalls.Load(), stopCalls.Load(), registered.Load())
	}
	if err := parent.Err(); err != nil {
		t.Fatalf("normal completion canceled the caller context: %v", err)
	}
}
