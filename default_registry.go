package flow

import "sync"

var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   *Registry
)

// SetDefaultRegistry installs a process-wide registry used by
// CompilePipeline when no explicit registry is passed via
// WithFlowRegistry.
//
// Intended to be called once at application boot:
//
//	reg := flow.NewRegistry()
//	_ = reg.Register(fsio.Library())
//	flow.SetDefaultRegistry(reg)
//
// Passing nil clears the default. Safe for concurrent use.
func SetDefaultRegistry(r *Registry) {
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	defaultRegistry = r
}

// DefaultRegistry returns the process-wide registry, if any.
// Returns nil when unset.
func DefaultRegistry() *Registry {
	defaultRegistryMu.RLock()
	defer defaultRegistryMu.RUnlock()
	return defaultRegistry
}
