package transport

import (
	"context"
	"fmt"

	"github.com/nexssp/flow/dslparse"
	"github.com/nexssp/kernel/action"
)

// FindTrigger wyszukuje akcję z bindingiem OnTrigger(protocol) w dowolnym rejestrze lub liście akcji.
func FindTrigger(target any, protocol string) (action.AnyAction, bool) {
	var actions []action.AnyAction

	switch v := target.(type) {
	case *action.Registry:
		if v != nil {
			actions = v.Actions()
		}
	case action.Library:
		actions = v.Actions
	case []action.AnyAction:
		actions = v
	}

	for _, candidate := range actions {
		if candidate == nil {
			continue
		}
		for _, binding := range candidate.GetBindings() {
			if trigger, ok := binding.(TriggerBinding); ok && trigger.Protocol == protocol {
				return candidate, true
			}
		}
	}
	return nil, false
}

// RequireTrigger zwraca akcję triggera lub błąd.
func RequireTrigger(target any, protocol string) (action.AnyAction, error) {
	if act, ok := FindTrigger(target, protocol); ok {
		return act, nil
	}
	return nil, fmt.Errorf("flow/transport: brak zarejestrowanego triggera dla protokołu %q", protocol)
}

// ResolveModifier finds the registered resolver for a parsed transport
// binding (for example :route= or :nats=) and passes the complete binding
// through so adapters retain protocol-specific fields.
func ResolveModifier(ctx context.Context, reg *action.Registry, binding *dslparse.TransportBinding) (action.Binding, bool, error) {
	if reg == nil {
		return nil, false, nil
	}
	if binding == nil || binding.Kind == "" {
		return nil, false, nil
	}

	for _, candidate := range reg.Actions() {
		if candidate == nil {
			continue
		}
		for _, candidateBinding := range candidate.GetBindings() {
			b, ok := candidateBinding.(DSLBinding)
			if !ok || b.Kind != binding.Kind {
				continue
			}
			result, err := candidate.DoAny(ctx, binding)
			if err != nil {
				return nil, true, fmt.Errorf("flow/transport: resolver %q: %w", binding.Kind, err)
			}
			resolved, ok := result.(action.Binding)
			if !ok {
				return nil, true, fmt.Errorf("flow/transport: resolver %q returned %T, want action.Binding", binding.Kind, result)
			}
			return resolved, true, nil
		}
	}
	return nil, false, nil
}
