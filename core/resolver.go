// Plik: flow/core/resolver.go
package core

import (
	"sort"
	"sync"

	"github.com/nexssp/kernel/action"
)

// CapabilityResolver odcina rdzeń kompilatora od twardej bazy akcji.
type CapabilityResolver interface {
	Action(name string) (action.AnyAction, bool)
	Stream(name string) (action.AnyStreamAction, bool)
	Operator(name string) (action.NamedOperator, bool)
	Mount(lib action.Library) error
	MountWithAlias(lib action.Library, alias string) error
}

type DynamicResolver struct {
	mu        sync.RWMutex
	actions   map[string]action.AnyAction
	streams   map[string]action.AnyStreamAction
	operators map[string]action.NamedOperator
	owners    map[string]resolverOwner
	mounts    map[string]string
}

func NewDynamicResolver(baseLibs ...action.Library) (*DynamicResolver, error) {
	r := &DynamicResolver{
		actions:   make(map[string]action.AnyAction),
		streams:   make(map[string]action.AnyStreamAction),
		operators: make(map[string]action.NamedOperator),
		owners:    make(map[string]resolverOwner),
		mounts:    make(map[string]string),
	}
	for i := range baseLibs {
		if err := r.Mount(baseLibs[i]); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *DynamicResolver) Action(name string) (action.AnyAction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.actions[name]
	return a, ok
}

func (r *DynamicResolver) Stream(name string) (action.AnyStreamAction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.streams[name]
	return s, ok
}

func (r *DynamicResolver) Operator(name string) (action.NamedOperator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	op, ok := r.operators[name]
	return op, ok
}

// ActionNames returns every canonical action name in stable order.
func (r *DynamicResolver) ActionNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.actions))
	for name := range r.actions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Actions returns each mounted action once, keyed by its canonical metadata name.
func (r *DynamicResolver) Actions() []action.AnyAction {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byName := make(map[string]action.AnyAction, len(r.actions))
	for _, act := range r.actions {
		if act == nil || act.Describe() == nil {
			continue
		}
		byName[act.Describe().Name] = act
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]action.AnyAction, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

// Streams returns mounted stream sources in stable order.
func (r *DynamicResolver) Streams() []action.AnyStreamAction {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.streams))
	for name := range r.streams {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]action.AnyStreamAction, 0, len(names))
	for _, name := range names {
		out = append(out, r.streams[name])
	}
	return out
}

// Operators returns mounted stream operators in stable order.
func (r *DynamicResolver) Operators() []action.NamedOperator {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.operators))
	for name := range r.operators {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]action.NamedOperator, 0, len(names))
	for _, name := range names {
		out = append(out, r.operators[name])
	}
	return out
}
