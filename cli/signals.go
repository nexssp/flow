package cli

import (
	"context"
	"os"
	"os/signal"
	"sync"
)

type signalRegistrar func(chan<- os.Signal, ...os.Signal) func()

// runCLIWithSignals wraps the complete Host lifetime in one OS-signal scope.
func runCLIWithSignals(parent context.Context, inv *invocation, args []string, register signalRegistrar) int {
	return withSignalContext(parent, register, func(ctx context.Context) int {
		return runInvocation(ctx, inv, func(runCtx context.Context) int {
			return runCLI(runCtx, inv, args)
		})
	})
}

// withSignalContext converts the first registered termination signal into
// cancellation, stopping signal delivery before cancellation starts draining
// work. Repeated signals then use the platform default behavior. SIGHUP and
// SIGQUIT are deliberately not registered; SIGKILL and SIGSTOP are uncatchable.
//
//nolint:contextcheck // A nil parent is normalized like Host.Run before deriving the signal context.
func withSignalContext(parent context.Context, register signalRegistrar, dispatch func(context.Context) int) int {
	if parent == nil {
		parent = context.Background()
	}
	if register == nil {
		register = registerProcessSignals
	}

	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	unregister := register(signals, commandSignals()...)
	if unregister == nil {
		unregister = func() {}
	}
	var unregisterOnce sync.Once
	stopSignals := func() { unregisterOnce.Do(unregister) }

	watcherStop := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			stopSignals()
		case <-watcherStop:
			stopSignals()
		case <-signals:
			stopSignals()
			cancel()
		}
	}()

	defer func() {
		close(watcherStop)
		stopSignals()
		cancel()
		<-watcherDone
	}()
	return dispatch(ctx)
}

func registerProcessSignals(signals chan<- os.Signal, catch ...os.Signal) func() {
	signal.Notify(signals, catch...)
	return func() { signal.Stop(signals) }
}
