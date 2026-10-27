package compiler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nexssp/kernel/xctx"
)

const runCleanupTimeout = 10 * time.Second

// RunCleanupFunc releases a resource registered for one Flow invocation.
// Cleanup receives a fresh bounded context even if the run was canceled.
type RunCleanupFunc func(context.Context) error

// ErrRunScopeClosed is returned when a resource is registered after cleanup
// has started.
var ErrRunScopeClosed = errors.New("compiler: run scope is closed")

type runScopeEntry struct {
	id    uint64
	close RunCleanupFunc
}

// RunScope is Flow-specific: xctx.NewScope provides pooled request metadata
// and a pool-release function, but no derived cancellation, resource
// registration, cleanup ordering, or bounded cleanup context.
// RunScope owns resources created during one program invocation. Close cancels
// the run context first, then invokes active cleanup callbacks in reverse
// registration order exactly once.
type RunScope struct {
	mu        sync.Mutex
	entries   []runScopeEntry
	nextID    uint64
	closed    bool
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
}

var runScopeKey = xctx.NewKey[*RunScope]("flow.run_scope")

// NewRunScope returns a context carrying a new per-invocation resource scope.
// A nil parent is treated as context.Background.
func NewRunScope(parent context.Context) (context.Context, *RunScope) {
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(parent)
	scope := &RunScope{cancel: cancel}
	return runScopeKey.With(runCtx, scope), scope
}

// RunScopeFromContext returns the invocation scope carried by ctx, if any.
func RunScopeFromContext(ctx context.Context) (*RunScope, bool) {
	return runScopeKey.From(ctx)
}

// Register adds a cleanup callback and returns a release function. Calling the
// release function removes the callback when the resource was closed earlier
// during normal streaming; it is safe to call more than once.
func (s *RunScope) Register(cleanup RunCleanupFunc) (release func(), err error) {
	if s == nil {
		return nil, errors.New("compiler: cannot register a resource in a nil run scope")
	}
	if cleanup == nil {
		return nil, errors.New("compiler: run cleanup callback is nil")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrRunScopeClosed
	}
	s.nextID++
	id := s.nextID
	s.entries = append(s.entries, runScopeEntry{id: id, close: cleanup})
	s.mu.Unlock()

	var once sync.Once
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed {
				return
			}
			for i := range s.entries {
				if s.entries[i].id == id {
					s.entries = append(s.entries[:i], s.entries[i+1:]...)
					return
				}
			}
		})
	}
	return release, nil
}

// Close cancels the run context and closes registered resources in reverse
// order. It is idempotent. Cleanup failures are joined and returned.
func (s *RunScope) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background() //nolint:contextcheck // Close explicitly accepts a nil context.
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		entries := append([]runScopeEntry(nil), s.entries...)
		s.mu.Unlock()

		s.cancel()
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runCleanupTimeout)
		defer cancel()

		var errs []error
		for _, entry := range slices.Backward(entries) {
			if err := invokeRunCleanup(cleanupCtx, entry.close); err != nil {
				errs = append(errs, fmt.Errorf("resource %d: %w", entry.id, err))
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

func invokeRunCleanup(ctx context.Context, cleanup RunCleanupFunc) (err error) {
	completed := false
	defer func() {
		if !completed {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("cleanup callback panicked: %v", recovered)
			}
		}
	}()
	err = cleanup(ctx)
	completed = true
	return err
}
