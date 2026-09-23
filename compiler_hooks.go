package flow

import (
	"fmt"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// resolveProfileHooks turns the profile's declared hook names into
// concrete AnyHook values, resolved from the registry.
//
// A profile that declares a hook the registry does not know about is a
// compile-time error, never a silent no-op: a flow that asks for
// isolation but cannot actually provide it must not run.
//
// The returned hooks are already wrapped with the security observer
// (when one is configured), so every firing emits a SecurityEvent.
func (c *Compiler) resolveProfileHooks(policy ProfilePolicy) ([]action.AnyHook, error) {
	if len(policy.DefaultHooks) == 0 {
		return nil, nil
	}

	if c.registry == nil {
		return nil, xerr.Internal(fmt.Sprintf(
			"flow: profile declares %d hook(s) but compiler has no registry",
			len(policy.DefaultHooks),
		))
	}

	hooks := make([]action.AnyHook, 0, len(policy.DefaultHooks))

	for _, name := range policy.DefaultHooks {
		act, ok := c.registry.Get(name)
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf(
				"flow: profile requires hook %q but it is not registered; "+
					"add the corresponding @require directive",
				name,
			))
		}

		provider, ok := act.(action.HookProvider)
		if !ok {
			return nil, xerr.Internal(fmt.Sprintf(
				"flow: profile hook %q does not expose AnyHooks",
				name,
			))
		}

		exposed := provider.GetAnyHooks()
		if len(exposed) == 0 {
			return nil, xerr.Internal(fmt.Sprintf(
				"flow: profile hook %q is registered but exposes no hooks",
				name,
			))
		}

		for _, h := range exposed {
			hooks = append(hooks, wrapProfileHook(h, name, c.secObs))
		}
	}

	return hooks, nil
}

// GateRules returns the @gate rules that were parsed into the
// Preprocessed structure. Provided as a helper so callers that hold a
// Preprocessed but not a compiler can still inspect the rules.
//
// This function does NOT belong in the compiler hot path; it exists
// only because a couple of tools (runner trace, doctor command) need
// to iterate gates without re-parsing.
func GateRules(pre *directives.Preprocessed) []directives.GateRule {
	if pre == nil {
		return nil
	}
	return pre.Gates
}
