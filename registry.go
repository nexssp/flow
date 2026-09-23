package flow

import (
	"fmt"
	"sort"

	"github.com/nexssp/kernel/action"
)

// Alias types to guarantee complete interoperability with the kernel without duplication.
type (
	Library        = action.Library
	NamedOperator  = action.NamedOperator
	StreamOperator = action.StreamOperator
)

// Kind identifies the role a registered entry plays in a pipeline.
type Kind uint8

const (
	KindUnary Kind = iota
	KindSource
	KindOperator
)

func (k Kind) String() string {
	switch k {
	case KindUnary:
		return "unary"
	case KindSource:
		return "source"
	case KindOperator:
		return "operator"
	default:
		return "unknown"
	}
}

// Registry holds registered entries by name.
type Registry struct {
	actions   map[string]action.AnyAction
	sources   map[string]action.AnyStreamAction
	operators map[string]action.NamedOperator
	owner     map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		actions:   make(map[string]action.AnyAction),
		sources:   make(map[string]action.AnyStreamAction),
		operators: make(map[string]action.NamedOperator),
		owner:     make(map[string]string),
	}
}

func (r *Registry) Register(lib Library) error {
	if r == nil {
		return fmt.Errorf("flow: nil registry")
	}

	seen := make(map[string]Kind, len(lib.Actions)+len(lib.Sources)+len(lib.Operators))

	checkName := func(name string) error {
		if name == "" {
			return fmt.Errorf("flow: library %q contains entry with empty name", lib.Name)
		}
		if prevKind, duplicate := seen[name]; duplicate {
			return fmt.Errorf("flow: library %q declares %q twice (%s and another kind)", lib.Name, name, prevKind)
		}
		if prevOwner, exists := r.owner[name]; exists {
			return fmt.Errorf("flow: name %q already registered by library %q (conflict with %q)", name, prevOwner, lib.Name)
		}
		return nil
	}

	for _, act := range lib.Actions {
		if act == nil {
			continue
		}
		meta := act.Describe()
		if meta == nil {
			return fmt.Errorf("flow: library %q has action with nil Meta", lib.Name)
		}
		if err := checkName(meta.Name); err != nil {
			return err
		}
		seen[meta.Name] = KindUnary
	}

	for _, src := range lib.Sources {
		if src == nil {
			continue
		}
		meta := src.Describe()
		if meta == nil {
			return fmt.Errorf("flow: library %q has source with nil Meta", lib.Name)
		}
		if err := checkName(meta.Name); err != nil {
			return err
		}
		seen[meta.Name] = KindSource
	}

	for _, op := range lib.Operators {
		if err := action.ValidateOperatorDeclaration(op); err != nil {
			return err
		}
		if err := checkName(op.Name); err != nil {
			return err
		}
		seen[op.Name] = KindOperator
	}

	// Commit entries
	for _, act := range lib.Actions {
		if act != nil {
			r.actions[act.Describe().Name] = act
			r.owner[act.Describe().Name] = lib.Name
		}
	}
	for _, src := range lib.Sources {
		if src != nil {
			r.sources[src.Describe().Name] = src
			r.owner[src.Describe().Name] = lib.Name
		}
	}
	for _, op := range lib.Operators {
		r.operators[op.Name] = op.Clone()
		r.owner[op.Name] = lib.Name
	}

	// Resolve aliases after every primary name is committed. Canonical
	// names always win; an alias is registered only when its target
	// resolves and the alias itself does not collide with an existing
	// name. This mirrors the kernel action registry's behavior, so a
	// short name that works in one registry works in the other.
	for _, alias := range lib.Aliases {
		if alias.Canonical == "" || len(alias.Short) == 0 {
			continue
		}
		target, ok := r.lookupAny(alias.Canonical)
		if !ok {
			continue
		}
		for _, short := range alias.Short {
			if short == "" || short == alias.Canonical {
				continue
			}
			if _, exists := r.lookupAny(short); exists {
				continue
			}
			r.registerAlias(short, target)
		}
	}

	return nil
}

func (r *Registry) Resolve(name string) (Kind, bool) {
	if r == nil {
		return 0, false
	}
	if _, ok := r.actions[name]; ok {
		return KindUnary, true
	}
	if _, ok := r.sources[name]; ok {
		return KindSource, true
	}
	if _, ok := r.operators[name]; ok {
		return KindOperator, true
	}
	return 0, false
}

func (r *Registry) Get(name string) (action.AnyAction, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.actions[name]
	return a, ok
}

func (r *Registry) GetStream(name string) (action.AnyStreamAction, bool) {
	if r == nil {
		return nil, false
	}
	s, ok := r.sources[name]
	return s, ok
}

func (r *Registry) GetOperator(name string) (NamedOperator, bool) {
	if r == nil {
		return NamedOperator{}, false
	}
	op, ok := r.operators[name]
	if !ok {
		return NamedOperator{}, false
	}
	return op.Clone(), true
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(r.actions)+len(r.sources)+len(r.operators))
	for n := range r.actions {
		seen[n] = struct{}{}
	}
	for n := range r.sources {
		seen[n] = struct{}{}
	}
	for n := range r.operators {
		seen[n] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) lookupAny(name string) (any, bool) {
	if a, ok := r.actions[name]; ok {
		return a, true
	}
	if s, ok := r.sources[name]; ok {
		return s, true
	}
	if op, ok := r.operators[name]; ok {
		return op, true
	}
	return nil, false
}

func (r *Registry) registerAlias(short string, target any) {
	switch v := target.(type) {
	case action.AnyAction:
		r.actions[short] = v
		r.owner[short] = "alias"
	case action.AnyStreamAction:
		r.sources[short] = v
		r.owner[short] = "alias"
	case action.NamedOperator:
		r.operators[short] = v
		r.owner[short] = "alias"
	}
}

// RegistryFromActionRegistry bridges a kernel action.Registry into a flow.Registry,
// copying actions, stream sources, and operators so stream pipelines resolve seamlessly.
func RegistryFromActionRegistry(actionRegistry *action.Registry) *Registry {
	flowRegistry := NewRegistry()
	if actionRegistry == nil {
		return flowRegistry
	}
	for _, name := range actionRegistry.Names() {
		if act, ok := actionRegistry.Get(name); ok {
			flowRegistry.actions[name] = act
			flowRegistry.owner[name] = "action_registry"
		}
		if source, ok := actionRegistry.GetStream(name); ok {
			flowRegistry.sources[name] = source
			flowRegistry.owner[name] = "action_registry"
		}
		if operator, ok := actionRegistry.GetOperator(name); ok {
			flowRegistry.operators[name] = operator
			flowRegistry.owner[name] = "action_registry"
		}
	}
	return flowRegistry
}
