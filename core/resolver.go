// Plik: flow/core/resolver.go
package core

import (
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
}

func NewDynamicResolver(baseLibs ...action.Library) (*DynamicResolver, error) {
	r := &DynamicResolver{
		actions:   make(map[string]action.AnyAction),
		streams:   make(map[string]action.AnyStreamAction),
		operators: make(map[string]action.NamedOperator),
	}
	for i := range baseLibs {
		if err := r.Mount(baseLibs[i]); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *DynamicResolver) Mount(lib action.Library) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, a := range lib.Actions {
		if a == nil || a.Describe() == nil {
			continue
		}
		if len(lib.Hooks) > 0 {
			a = a.CloneWithHooks(lib.Hooks...)
		}
		r.actions[a.Describe().Name] = a
	}

	for _, s := range lib.Sources {
		if s == nil || s.Describe() == nil {
			continue
		}
		if len(lib.Hooks) > 0 {
			s = s.CloneWithHooks(lib.Hooks...)
		}
		r.streams[s.Describe().Name] = s
	}

	for _, op := range lib.Operators {
		r.operators[op.Name] = op.Clone()
	}

	for _, alias := range lib.Aliases {
		if target, ok := r.actions[alias.Canonical]; ok {
			for _, short := range alias.Short {
				if short != "" && short != alias.Canonical {
					r.actions[short] = target
				}
			}
		}
	}
	return nil
}

type aliasedStream struct {
	action.AnyStreamAction
	alias string
}

func (a *aliasedStream) Describe() *action.Meta {
	m := *a.AnyStreamAction.Describe()
	m.Name = a.alias + "." + m.Name
	return &m
}

// MountWithAlias montuje bibliotekę dodając podany prefiks (alias)
// do wszystkich jej akcji, strumieni, operatorów i zdefiniowanych aliasów.
func (r *DynamicResolver) MountWithAlias(lib action.Library, alias string) error {
	if alias == "" {
		return r.Mount(lib)
	}

	lib.Name = alias + "." + lib.Name

	for i, a := range lib.Actions {
		if a == nil || a.Describe() == nil {
			continue
		}
		oldName := a.Describe().Name
		lib.Actions[i] = action.Dynamic(a).Name(alias + "." + oldName).Build()
	}

	for i, s := range lib.Sources {
		if s == nil || s.Describe() == nil {
			continue
		}
		lib.Sources[i] = &aliasedStream{AnyStreamAction: s, alias: alias}
	}

	for i, op := range lib.Operators {
		cloned := op.Clone()
		cloned.Name = alias + "." + cloned.Name
		lib.Operators[i] = cloned
	}

	for i, al := range lib.Aliases {
		lib.Aliases[i].Canonical = alias + "." + al.Canonical
		for j, sh := range al.Short {
			lib.Aliases[i].Short[j] = alias + "." + sh
		}
	}

	return r.Mount(lib)
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
