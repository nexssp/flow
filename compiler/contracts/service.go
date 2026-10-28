package contracts

import (
	"context"
	"sync"

	"github.com/nexssp/kernel/xctx"
)

var serviceReadyKey = xctx.NewKey[*ReadySignal]("flow.contracts.service_ready")

// ReadySignal is a one-shot readiness barrier for a long-lived service.
// MarkStopped prevents a late readiness notification after the service exits.
type ReadySignal struct {
	mu      sync.Mutex
	ch      chan struct{}
	ready   bool
	stopped bool
}

// NewReadySignal creates a readiness barrier.
func NewReadySignal() *ReadySignal {
	return &ReadySignal{ch: make(chan struct{})}
}

// Done returns the channel closed when the service becomes ready.
func (s *ReadySignal) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.ch
}

// MarkReady signals readiness once, unless the service has already stopped.
func (s *ReadySignal) MarkReady() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.ready {
		return
	}
	s.ready = true
	close(s.ch)
}

// MarkStopped makes later MarkReady calls no-ops. It does not close Done:
// dependents are released by the supervisor's cancellation on service exit.
func (s *ReadySignal) MarkStopped() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// WithServiceReady binds a readiness signal to a service invocation context.
func WithServiceReady(ctx context.Context, signal *ReadySignal) context.Context {
	if ctx == nil || signal == nil {
		return ctx
	}
	return serviceReadyKey.With(ctx, signal)
}

// SignalReady notifies the supervisor that the current service is ready.
// It is a no-op for standalone executions that have no bound service signal.
func SignalReady(ctx context.Context) {
	if ctx == nil {
		return
	}
	if signal, ok := serviceReadyKey.From(ctx); ok && signal != nil {
		signal.MarkReady()
	}
}
