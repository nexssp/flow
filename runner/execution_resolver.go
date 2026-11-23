package runner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
)

type executionResolver struct {
	base  compiler.CapabilityResolver
	hooks []action.AnyHook

	mu sync.Mutex

	mountedActions   map[string]action.AnyAction
	mountedStreams   map[string]action.AnyStreamAction
	mountedOperators map[string]action.NamedOperator
	owners           map[string]executionOwner
	mounts           map[string]string
	contracts        map[string]compiler.CapabilityContract
	secretContracts  *secretContractRegistry

	actionsSnapshot   []action.AnyAction
	streamsSnapshot   []action.AnyStreamAction
	operatorsSnapshot []action.NamedOperator
}

type executionOwner struct {
	kind    string
	library string
}

const baseResolverOwner = "base resolver"

func (r *executionResolver) baseOwner(name string) (executionOwner, bool) {
	if _, ok := r.base.Action(name); ok {
		return executionOwner{kind: "action", library: baseResolverOwner}, true
	}
	if _, ok := r.base.Stream(name); ok {
		return executionOwner{kind: "source", library: baseResolverOwner}, true
	}
	if _, ok := r.base.Operator(name); ok {
		return executionOwner{kind: "operator", library: baseResolverOwner}, true
	}
	return executionOwner{}, false
}

func newExecutionResolver(base compiler.CapabilityResolver, hooks []action.AnyHook) (compiler.CapabilityResolver, error) {
	return newExecutionResolverWithSecretContracts(base, hooks, nil, nil)
}

func newExecutionResolverWithSecretContracts(base compiler.CapabilityResolver, hooks []action.AnyHook, secretContracts *secretContractRegistry, scrubber *secret.Scrubber) (compiler.CapabilityResolver, error) {
	if base == nil {
		return nil, errors.New("executionResolver: base resolver is nil")
	}
	safeHooks := make([]action.AnyHook, 0, len(hooks)+1)
	for _, hook := range hooks {
		safeHooks = append(safeHooks, secretSanitizingHook(hook, secretContracts, scrubber))
	}
	// The secret capture hook is appended without the sanitizing wrapper: it
	// only records secret values for later scrubbing and never reads event
	// payloads, so wrapping it would reflectively walk every payload for a
	// hook that ignores them.
	safeHooks = append(safeHooks, secretCaptureHook(scrubber, secretContracts))
	// Materializers mount declarations into the resolver, so each execution
	// needs an overlay even when no hooks are configured.
	r := &executionResolver{
		base:             base,
		hooks:            safeHooks,
		mountedActions:   make(map[string]action.AnyAction),
		mountedStreams:   make(map[string]action.AnyStreamAction),
		mountedOperators: make(map[string]action.NamedOperator),
		owners:           make(map[string]executionOwner),
		mounts:           make(map[string]string),
		contracts:        make(map[string]compiler.CapabilityContract),
		secretContracts:  secretContracts,
	}
	if lister, ok := base.(interface{ Actions() []action.AnyAction }); ok {
		for _, act := range lister.Actions() {
			if act != nil && act.Describe() != nil {
				r.owners[act.Describe().Name] = executionOwner{kind: "action", library: baseResolverOwner}
			}
		}
	}
	if lister, ok := base.(interface {
		Streams() []action.AnyStreamAction
	}); ok {
		for _, src := range lister.Streams() {
			if src != nil && src.Describe() != nil {
				r.owners[src.Describe().Name] = executionOwner{kind: "source", library: baseResolverOwner}
			}
		}
	}
	if lister, ok := base.(interface{ Operators() []action.NamedOperator }); ok {
		for _, op := range lister.Operators() {
			r.owners[op.Name] = executionOwner{kind: "operator", library: baseResolverOwner}
		}
	}
	return r, nil
}

func (r *executionResolver) Contract(name string) (compiler.CapabilityContract, bool) {
	r.mu.Lock()
	if contract, ok := r.contracts[name]; ok {
		r.mu.Unlock()
		return contract, true
	}
	act, actionFound := r.actionLocked(name)
	base := r.base
	r.mu.Unlock()
	if provider, ok := base.(compiler.ContractProvider); ok {
		if contract, found := provider.Contract(name); found {
			return contract, true
		}
	}
	if actionFound {
		return compiler.ActionContract(name, act), true
	}
	return compiler.CapabilityContract{}, false
}

func (r *executionResolver) RegisterContract(name string, contract compiler.CapabilityContract) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actionLocked(name); !ok {
		return fmt.Errorf("executionResolver: cannot register a contract for unknown action %q", name)
	}
	contract.Name = name
	if previous, exists := r.contracts[name]; exists && !reflect.DeepEqual(previous, contract) {
		return fmt.Errorf("executionResolver: action %q already has a conflicting contract", name)
	}
	r.contracts[name] = contract
	if r.secretContracts != nil {
		r.secretContracts.register(name, contract.Input.Type, contract.Output.Type)
	}
	return nil
}

var (
	_ compiler.ContractProvider  = (*executionResolver)(nil)
	_ compiler.ContractRegistrar = (*executionResolver)(nil)
)

// secretCaptureHook remembers secret values seen in action payloads so echoed
// values can be scrubbed from later outputs and errors. It replaces the former
// observe.Hook(secretCaptureSink{}) wiring: the no-op sink existed only to route
// RecordExecutionStart and secret capture through the sanitizing wrapper, whose
// full payload sanitization protected nothing because no callback of this hook
// ever reads a request or response. Capture itself still walks payloads; that
// walk is what keeps the execution-scoped scrubber able to redact secrets later.
func secretCaptureHook(scrubber *secret.Scrubber, contracts *secretContractRegistry) action.AnyHook {
	capture := func(ctx context.Context, actionName string, request, response, recovered any) {
		active := activeSecretScrubber(ctx, scrubber)
		if active == nil {
			return
		}
		var requestType, responseType reflect.Type
		if contracts != nil {
			if contract, ok := contracts.contractTypes(actionName); ok {
				requestType, responseType = contract.request, contract.response
			}
		}
		active.Capture(request, requestType)
		active.Capture(response, responseType)
		active.Capture(recovered, nil)
	}
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			return action.RecordExecutionStart(ctx), nil
		},
		OnSuccess: func(ctx context.Context, request, response any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, response, nil)
		},
		OnTimeout: func(ctx context.Context, request any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnError: func(ctx context.Context, request any, _ error, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnRetry: func(ctx context.Context, request any, _ int, _ error, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnCacheHit: func(ctx context.Context, request, response any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, response, nil)
		},
		OnCacheMiss: func(ctx context.Context, request any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnCoalesced: func(ctx context.Context, request any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnDeduplicated: func(ctx context.Context, request any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnCancel: func(ctx context.Context, request any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, nil)
		},
		OnPanic: func(ctx context.Context, request, recovered any, meta *action.Meta) {
			capture(ctx, hookActionName(meta), request, nil, recovered)
		},
	}
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
	if err := r.mountLocked(lib, "", r.hooks); err != nil {
		return err
	}
	r.invalidateAllSnapshotsLocked()
	return nil
}

func (r *executionResolver) MountWithAlias(lib action.Library, alias string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.mountLocked(lib, alias, r.hooks); err != nil {
		return err
	}
	r.invalidateAllSnapshotsLocked()
	return nil
}

func (r *executionResolver) mountLocked(lib action.Library, alias string, hooks []action.AnyHook) error {
	stage, err := compiler.NewDynamicResolver()
	if err != nil {
		return err
	}
	if alias == "" {
		err = stage.Mount(lib)
	} else {
		err = stage.MountWithAlias(lib, alias)
	}
	if err != nil {
		return err
	}
	mountKey := lib.Name
	owner := lib.Name
	if alias != "" {
		mountKey += "\x00" + alias
		owner = fmt.Sprintf("%s as %s", lib.Name, alias)
	}
	if previous, exists := r.mounts[mountKey]; exists {
		return fmt.Errorf("duplicate mount of library %q with qualifier %q (previous owner %q)", lib.Name, alias, previous)
	}

	pending := make(map[string]executionOwner)
	check := func(name, kind string) error {
		previous, exists := r.owners[name]
		if !exists {
			previous, exists = r.baseOwner(name)
		}
		if exists {
			return fmt.Errorf("canonical collision: name %q kind %s owner %q conflicts with kind %s owner %q",
				name, kind, owner, previous.kind, previous.library)
		}
		if previous, exists := pending[name]; exists {
			return fmt.Errorf("canonical collision: name %q kind %s owner %q conflicts with kind %s owner %q in the same library mount",
				name, kind, owner, previous.kind, previous.library)
		}
		pending[name] = executionOwner{kind: kind, library: owner}
		return nil
	}
	for _, act := range stage.Actions() {
		name := act.Describe().Name
		if err := check(name, "action"); err != nil {
			return err
		}
	}
	for _, src := range stage.Streams() {
		name := src.Describe().Name
		if err := check(name, "source"); err != nil {
			return err
		}
	}
	for _, op := range stage.Operators() {
		if err := check(op.Name, "operator"); err != nil {
			return err
		}
	}

	for _, act := range stage.Actions() {
		name := act.Describe().Name
		r.mountedActions[name] = act.CloneWithHooks(hooks...)
		r.owners[name] = pending[name]
	}
	for _, src := range stage.Streams() {
		name := src.Describe().Name
		r.mountedStreams[name] = src.CloneWithHooks(hooks...)
		r.owners[name] = pending[name]
	}
	for _, op := range stage.Operators() {
		r.mountedOperators[op.Name] = op.Clone()
		r.owners[op.Name] = pending[op.Name]
	}
	r.mounts[mountKey] = owner
	return nil
}

func (r *executionResolver) invalidateAllSnapshotsLocked() {
	r.actionsSnapshot = nil
	r.streamsSnapshot = nil
	r.operatorsSnapshot = nil
}

var _ compiler.CapabilityResolver = (*executionResolver)(nil)
