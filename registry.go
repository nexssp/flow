package flow

import (
	"fmt"
	"sync"

	"github.com/nexssp/kernel/action"
)

type Registry interface {
	Get(name string) (action.AnyAction, bool)
	Actions() []action.AnyAction
}

type MapRegistry struct {
	mu      sync.RWMutex
	actions map[string]action.AnyAction
}

var _ Registry = (*MapRegistry)(nil)

func NewRegistry(actions ...action.AnyAction) *MapRegistry {
	m := make(map[string]action.AnyAction, len(actions))
	for _, a := range actions {
		if a != nil {
			m[a.Describe().Name] = a
		}
	}

	return &MapRegistry{actions: m}
}

func (r *MapRegistry) Get(capability string) (action.AnyAction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.actions[capability]

	return a, ok
}

func (r *MapRegistry) Register(capability string, a action.AnyAction) {
	if a == nil || capability == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.actions[capability] = a
}

func (r *MapRegistry) Actions() []action.AnyAction {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]action.AnyAction, 0, len(r.actions))
	for _, a := range r.actions {
		out = append(out, a)
	}

	return out
}

func (r *MapRegistry) CompilePipeline(expr string) (action.Executable, error) {
	if r == nil {
		return nil, fmt.Errorf("flow: nil registry")
	}

	builder, err := CompilePipeline(expr, r)
	if err != nil {
		return nil, err
	}

	return builder.Build(), nil
}
