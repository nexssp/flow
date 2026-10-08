package decide

import (
	"slices"
	"sync"

	"github.com/nexssp/kernel/xerr"
)

// Registry is an in-process backend table. It is a plain struct, not a
// package-level variable, so tests can create isolated instances.
type Registry struct {
	mutex    sync.RWMutex
	backends map[string]Backend
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{backends: make(map[string]Backend)}
}

// Register adds a backend. A duplicate name is an error, not a
// silent override.
func (r *Registry) Register(backend Backend) error {
	if backend == nil {
		return xerr.BadRequest("decide: backend cannot be nil")
	}
	name := backend.Name()
	if name == "" {
		return xerr.BadRequest("decide: backend name cannot be empty")
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()
	if _, exists := r.backends[name]; exists {
		return xerr.Conflict("decide: duplicate backend " + name)
	}
	r.backends[name] = backend
	return nil
}

// Lookup returns the named backend.
func (r *Registry) Lookup(name string) (Backend, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	backend, exists := r.backends[name]
	return backend, exists
}

// Names returns every registered backend name, sorted.
func (r *Registry) Names() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	names := make([]string, 0, len(r.backends))
	for name := range r.backends {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

var defaultRegistry = NewRegistry()

// Register adds a backend to the process-wide registry.
func Register(backend Backend) error { return defaultRegistry.Register(backend) }

// Lookup resolves a backend by name from the process-wide registry.
func Lookup(name string) (Backend, bool) { return defaultRegistry.Lookup(name) }
