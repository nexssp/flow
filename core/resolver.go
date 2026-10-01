// Plik: flow/core/resolver.go
package core

import (
	"slices"
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

	lib = cloneLibrary(lib)
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

// cloneLibrary copies the library metadata and all mutable slice backing
// arrays. MountWithAlias rewrites names and aliases on its working copy;
// callers may safely reuse the original library for another resolver or
// another mount operation.
func cloneLibrary(lib action.Library) action.Library {
	lib.Actions = slices.Clone(lib.Actions)
	lib.Sources = slices.Clone(lib.Sources)
	lib.Operators = slices.Clone(lib.Operators)
	lib.Hooks = slices.Clone(lib.Hooks)
	lib.Overrides = slices.Clone(lib.Overrides)
	if len(lib.Aliases) > 0 {
		lib.Aliases = slices.Clone(lib.Aliases)
		for i := range lib.Aliases {
			lib.Aliases[i].Short = slices.Clone(lib.Aliases[i].Short)
		}
	}
	return lib
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

// ActionNames returns every canonical and aliased action name in stable order.
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
