package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/nexssp/flow/core"
)

const defaultShutdownTimeout = 10 * time.Second

// ErrHostShutdownStarted is returned when resources are adopted after the
// host has begun shutting down, or when Run is called on a closed host.
var ErrHostShutdownStarted = errors.New("runner: host shutdown has started")

// ErrHostAlreadyRunning is returned when another outer invocation is active.
var ErrHostAlreadyRunning = errors.New("runner: host invocation is already running")

// ErrHostInvocationRunning is returned when shutdown is requested before the
// outer invocation callback has completed.
var ErrHostInvocationRunning = errors.New("runner: host invocation is still running")

// HostOption configures an invocation host.
type HostOption func(*hostOptions)

type hostOptions struct {
	shutdownTimeout time.Duration
}

// WithShutdownTimeout sets the maximum time available to bundle cleanup.
// A non-positive timeout selects the conservative default.
func WithShutdownTimeout(timeout time.Duration) HostOption {
	return func(opts *hostOptions) {
		opts.shutdownTimeout = timeout
	}
}

// Host owns bundle cleanup callbacks for one outer synchronous invocation.
// It is intentionally separate from Config so repeated Compile/Execute calls
// can safely share a configuration and its resources.
type Host struct {
	mu              sync.Mutex
	callbacks       []core.ShutdownFunc
	shuttingDown    bool
	running         bool
	shutdownTimeout time.Duration
	shutdownOnce    sync.Once
	shutdownErr     error
}

// NewHost creates an invocation host.
func NewHost(opts ...HostOption) *Host {
	options := hostOptions{shutdownTimeout: defaultShutdownTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.shutdownTimeout <= 0 {
		options.shutdownTimeout = defaultShutdownTimeout
	}
	return &Host{shutdownTimeout: options.shutdownTimeout}
}

// Own transfers the bundle's cleanup callbacks to this host. Call it once for
// each constructed bundle instance, immediately after construction; bundle IDs
// are not identities and are deliberately not used for deduplication.
func (h *Host) Own(bundle core.Bundle) error {
	return h.OwnAll([]core.Bundle{bundle})
}

// OwnAll transfers every bundle's cleanup callbacks to this host in slice
// order. Shutdown invokes all callbacks in reverse order, including callbacks
// on bundles sharing the same ID.
func (h *Host) OwnAll(bundles []core.Bundle) error {
	if h == nil {
		return errors.New("runner: cannot adopt bundles into a nil host")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shuttingDown {
		return ErrHostShutdownStarted
	}
	for i := range bundles {
		for _, shutdown := range bundles[i].Shutdowns {
			if shutdown != nil {
				h.callbacks = append(h.callbacks, shutdown)
			}
		}
	}
	return nil
}

// Run is the outer synchronous lifetime for an invocation. It always shuts
// down after fn, joins cleanup errors after the primary error, and re-panics
// with the original panic value if fn panics.
func (h *Host) Run(ctx context.Context, fn func(context.Context) error) (result error) {
	if h == nil {
		return errors.New("runner: cannot run with a nil host")
	}
	if fn == nil {
		return errors.New("runner: host callback is nil")
	}
	if ctx == nil {
		//nolint:contextcheck // The public lifecycle API treats a nil context as the default context.
		ctx = context.Background()
	}

	h.mu.Lock()
	if h.shuttingDown {
		h.mu.Unlock()
		return ErrHostShutdownStarted
	}
	if h.running {
		h.mu.Unlock()
		return ErrHostAlreadyRunning
	}
	h.running = true
	h.mu.Unlock()

	returned := false
	defer func() {
		panicValue := recover()
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()
		if !returned {
			shutdownErr := h.Shutdown(ctx)
			if shutdownErr != nil {
				slog.Error("flow host shutdown failed while unwinding panic", "error", shutdownErr)
			}
			if panicValue != nil {
				panic(panicValue)
			}
			// This also lets runtime.Goexit continue after cleanup.
			return
		}
		result = joinShutdownError(result, h.Shutdown(ctx))
	}()

	result = fn(ctx)
	returned = true
	return result
}

// Shutdown invokes every owned callback exactly once in reverse ownership
// order. Callbacks receive a fresh deadline context that retains values from
// ctx but is unaffected by its cancellation or deadline. Cleanup callbacks
// must cooperate with that context; Go cannot forcibly stop a callback that
// ignores cancellation without abandoning a goroutine.
func (h *Host) Shutdown(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		//nolint:contextcheck // Shutdown must still clean owned resources for a nil context.
		ctx = context.Background()
	}
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return ErrHostInvocationRunning
	}
	h.shuttingDown = true
	h.mu.Unlock()
	h.shutdownOnce.Do(func() {
		h.mu.Lock()
		callbacks := append([]core.ShutdownFunc(nil), h.callbacks...)
		timeout := h.shutdownTimeout
		h.mu.Unlock()

		base := context.WithoutCancel(ctx)
		shutdownCtx, cancel := context.WithTimeout(base, timeout)
		defer cancel()

		var errs []error
		for i, shutdown := range slices.Backward(callbacks) {
			if err := invokeShutdown(shutdownCtx, shutdown); err != nil {
				errs = append(errs, fmt.Errorf("callback %d: %w", i, err))
			}
		}
		h.shutdownErr = errors.Join(errs...)
	})
	return h.shutdownErr
}

func invokeShutdown(ctx context.Context, shutdown core.ShutdownFunc) (err error) {
	completed := false
	defer func() {
		if !completed {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("cleanup callback panicked: %v", recovered)
			}
		}
	}()
	err = shutdown(ctx)
	completed = true
	return err
}

func joinShutdownError(primary, shutdownErr error) error {
	if shutdownErr == nil {
		return primary
	}
	wrapped := fmt.Errorf("flow shutdown: %w", shutdownErr)
	if primary == nil {
		return wrapped
	}
	return errors.Join(primary, wrapped)
}
