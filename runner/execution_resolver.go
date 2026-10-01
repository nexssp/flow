package runner

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

type executionResolver struct {
	base  core.CapabilityResolver
	hooks []action.AnyHook

	mu sync.Mutex

	mountedActions   map[string]action.AnyAction
	mountedStreams   map[string]action.AnyStreamAction
	mountedOperators map[string]action.NamedOperator

	actionsSnapshot   []action.AnyAction
	streamsSnapshot   []action.AnyStreamAction
	operatorsSnapshot []action.NamedOperator
}

func newExecutionResolver(base core.CapabilityResolver, hooks []action.AnyHook) (core.CapabilityResolver, error) {
	if base == nil {
		return nil, errors.New("executionResolver: base resolver is nil")
	}
	if len(hooks) == 0 {
		return base, nil
	}
	return &executionResolver{
		base:             base,
		hooks:            slices.Clone(hooks),
		mountedActions:   make(map[string]action.AnyAction),
		mountedStreams:   make(map[string]action.AnyStreamAction),
		mountedOperators: make(map[string]action.NamedOperator),
	}, nil
}

func (r *executionResolver) Action(name string) (action.AnyAction, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actionLocked(name)
}

func (r *executionResolver) actionLocked(name string) (action.AnyAction, bool) {
	if act, ok := r.mountedActions[name]; ok {
		return act, true
	}
	base, ok := r.base.Action(name)
	if !ok {
		return nil, false
	}
	clone := base.CloneWithHooks(r.hooks...)
	r.mountedActions[name] = clone
	r.actionsSnapshot = nil
	return clone, true
}

func (r *executionResolver) Actions() []action.AnyAction {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.actionsSnapshot != nil {
		return r.actionsSnapshot
	}

	var baseNames []string
	if lister, ok := r.base.(interface{ Actions() []action.AnyAction }); ok {
		actions := lister.Actions()
		baseNames = make([]string, len(actions))
		for i := range actions {
			baseNames[i] = actions[i].Describe().Name
		}
	}

	r.actionsSnapshot = buildSnapshot(baseNames, r.mountedActions, r.actionLocked)
	return r.actionsSnapshot
}

func (r *executionResolver) Stream(name string) (action.AnyStreamAction, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.streamLocked(name)
}

func (r *executionResolver) streamLocked(name string) (action.AnyStreamAction, bool) {
	if s, ok := r.mountedStreams[name]; ok {
		return s, true
	}
	base, ok := r.base.Stream(name)
	if !ok {
		return nil, false
	}
	clone := base.CloneWithHooks(r.hooks...)
	r.mountedStreams[name] = clone
	r.streamsSnapshot = nil
	return clone, true
}

func (r *executionResolver) Streams() []action.AnyStreamAction {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.streamsSnapshot != nil {
		return r.streamsSnapshot
	}

	var baseNames []string
	if lister, ok := r.base.(interface {
		Streams() []action.AnyStreamAction
	}); ok {
		streams := lister.Streams()
		baseNames = make([]string, len(streams))
		for i := range streams {
			baseNames[i] = streams[i].Describe().Name
		}
	}

	r.streamsSnapshot = buildSnapshot(baseNames, r.mountedStreams, r.streamLocked)
	return r.streamsSnapshot
}

func (r *executionResolver) Operator(name string) (action.NamedOperator, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.operatorLocked(name)
}

func (r *executionResolver) operatorLocked(name string) (action.NamedOperator, bool) {
	if op, ok := r.mountedOperators[name]; ok {
		return op, true
	}
	base, ok := r.base.Operator(name)
	if !ok {
		return action.NamedOperator{}, false
	}
	clone := base.Clone()
	r.mountedOperators[name] = clone
	r.operatorsSnapshot = nil
	return clone, true
}

func (r *executionResolver) Operators() []action.NamedOperator {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.operatorsSnapshot != nil {
		return r.operatorsSnapshot
	}

	var baseNames []string
	if lister, ok := r.base.(interface{ Operators() []action.NamedOperator }); ok {
		ops := lister.Operators()
		baseNames = make([]string, len(ops))
		for i := range ops {
			baseNames[i] = ops[i].Name
		}
	}

	r.operatorsSnapshot = buildSnapshot(baseNames, r.mountedOperators, r.operatorLocked)
	return r.operatorsSnapshot
}

// buildSnapshot is a DRY generic helper that deduplicates base+mounted entities.
func buildSnapshot[T any](baseNames []string, mounted map[string]T, getLocked func(string) (T, bool)) []T {
	names := append([]string(nil), baseNames...)
	for name := range mounted {
		names = append(names, name)
	}

	seen := make(map[string]bool, len(names))
	out := make([]T, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		if item, ok := getLocked(name); ok {
			out = append(out, item)
		}
	}
	return out
}

func (r *executionResolver) Mount(lib action.Library) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mountLocked(lib, "")
	r.invalidateAllSnapshotsLocked()
	return nil
}

func (r *executionResolver) MountWithAlias(lib action.Library, alias string) error {
	if alias == "" {
		return r.Mount(lib)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mountLocked(lib, alias+".")
	r.invalidateAllSnapshotsLocked()
	return nil
}

func (r *executionResolver) mountLocked(lib action.Library, prefix string) {
	combined := combineHooks(lib.Hooks, r.hooks)

	for i := range lib.Actions {
		act := lib.Actions[i]
		if act == nil {
			continue
		}
		name := act.Describe().Name
		if prefix != "" {
			act = action.Dynamic(act).Name(prefix + name).Build()
		}
		r.mountedActions[prefix+name] = act.CloneWithHooks(combined...)
	}

	for i := range lib.Sources {
		s := lib.Sources[i]
		if s == nil {
			continue
		}
		name := s.Describe().Name
		clone := s.CloneWithHooks(combined...)
		if prefix != "" {
			clone = &aliasedStream{inner: clone, alias: prefix[:len(prefix)-1]}
		}
		r.mountedStreams[prefix+name] = clone
	}

	for i := range lib.Operators {
		op := lib.Operators[i]
		cloned := op.Clone()
		if prefix != "" {
			cloned.Name = prefix + cloned.Name
		}
		r.mountedOperators[cloned.Name] = cloned
	}

	for i := range lib.Aliases {
		al := lib.Aliases[i]
		canonical := prefix + al.Canonical
		target, ok := r.mountedActions[canonical]
		if !ok {
			continue
		}
		for _, short := range al.Short {
			if short == "" || short == al.Canonical {
				continue
			}
			r.mountedActions[prefix+short] = target
		}
	}
}

func (r *executionResolver) invalidateAllSnapshotsLocked() {
	r.actionsSnapshot = nil
	r.streamsSnapshot = nil
	r.operatorsSnapshot = nil
}

func combineHooks(libHooks, execHooks []action.AnyHook) []action.AnyHook {
	switch {
	case len(libHooks) == 0 && len(execHooks) == 0:
		return nil
	case len(libHooks) == 0:
		return execHooks
	case len(execHooks) == 0:
		return libHooks
	default:
		out := make([]action.AnyHook, 0, len(libHooks)+len(execHooks))
		out = append(out, libHooks...)
		out = append(out, execHooks...)
		return out
	}
}

type aliasedStream struct {
	inner action.AnyStreamAction
	alias string
}

func (a *aliasedStream) Describe() *action.Meta {
	m := *a.inner.Describe()
	m.Name = a.alias + "." + m.Name
	return &m
}
func (a *aliasedStream) ReqPayload() any                { return a.inner.ReqPayload() }
func (a *aliasedStream) ResPayload() any                { return a.inner.ResPayload() }
func (a *aliasedStream) GetBindings() []action.Binding  { return a.inner.GetBindings() }
func (a *aliasedStream) GetAnyHooks() []action.AnyHook  { return a.inner.GetAnyHooks() }
func (a *aliasedStream) AddAnyHook(h ...action.AnyHook) { a.inner.AddAnyHook(h...) }
func (a *aliasedStream) CloneWithHooks(h ...action.AnyHook) action.AnyStreamAction {
	return &aliasedStream{inner: a.inner.CloneWithHooks(h...), alias: a.alias}
}

func (a *aliasedStream) DoStreamAny(ctx context.Context, req any) (action.AnyStream, error) {
	return a.inner.DoStreamAny(ctx, req)
}

var _ core.CapabilityResolver = (*executionResolver)(nil)
