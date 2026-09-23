package flow

import (
	"github.com/nexssp/flow/dslparse"
	"github.com/nexssp/kernel/action"
)

// ServiceResolver lets a domain package translate action modifiers
// into a concrete action implementation without flow knowing anything
// about the domain.
type ServiceResolver interface {
	Match(actionName string, mods dslparse.Modifiers) bool
	Resolve(actionName string, mods dslparse.Modifiers, act action.AnyAction) (action.AnyAction, error)
}

var resolvers []ServiceResolver

// RegisterResolver is called from init() of a domain package.
// Resolvers are tried in registration order.
func RegisterResolver(r ServiceResolver) {
	if r == nil {
		panic("flow: RegisterResolver(nil)")
	}
	resolvers = append(resolvers, r)
}

// ResetResolvers removes every registered resolver and returns the
// count that was removed. It exists so that tests can isolate
// themselves from resolvers registered by other tests in the same
// binary; production code has no reason to call it.
func ResetResolvers() int {
	n := len(resolvers)
	resolvers = nil
	return n
}

// resolveService walks the resolver list and returns the first result.
// When no resolver matches, act is returned unchanged.
func resolveService(name string, mods dslparse.Modifiers, act action.AnyAction) (action.AnyAction, error) {
	for _, r := range resolvers {
		if r.Match(name, mods) {
			return r.Resolve(name, mods, act)
		}
	}
	return act, nil
}
