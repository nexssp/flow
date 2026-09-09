package flow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type DynamicPolicy struct {
	RetryCount int
	Timeout    time.Duration
	Idempotent bool
	Breaker    bool
}

// resolveDynamicNode applies AST attributes (Profile, Targets, Params, Modifiers) to a retrieved capability.
func resolveDynamicNode(atom *compiler.AtomExpr, reg Registry) (*action.Builder[any, any], error) {
	act, ok := reg.Get(atom.Name)
	if !ok {
		return nil, xerr.NotFound(fmt.Sprintf("flow: capability %q not found in registry", atom.Name))
	}

	dyn := action.Dynamic(act)
	policy := parseModifiers(atom.Modifiers)

	// Apply compile-time policy decorations onto the builder
	if policy.Timeout > 0 {
		dyn.Timeout(policy.Timeout)
	}
	if policy.RetryCount > 0 {
		dyn.Retry(policy.RetryCount, action.ExponentialJitter(20*time.Millisecond, 500*time.Millisecond))
	}
	if policy.Idempotent {
		dyn.Idempotent()
	}

	// Consolidate parameters, inputs, and standard prompt/targets routing
	hasHooks := len(atom.Params) > 0 || len(atom.Inputs) > 0 || atom.Prompt != "" || len(atom.Targets) > 0 || len(atom.Excludes) > 0 || atom.Profile != ""

	if hasHooks {
		dyn.HookBefore(func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			if m, ok := req.(map[string]any); ok {
				for k, v := range atom.Params {
					m[k] = v // explicit literal bounds (e.g. env="staging")
				}
				// In a full implementation, atom.Inputs paths would be resolved here via state.Get()
				if atom.Prompt != "" {
					m["prompt"] = atom.Prompt
				}
				if len(atom.Targets) > 0 {
					m["targets"] = atom.Targets
				}
				if len(atom.Excludes) > 0 {
					m["excludes"] = atom.Excludes
				}
				if atom.Profile != "" {
					m["profile"] = atom.Profile
				}
			}
			return ctx, nil
		})
	}

	return dyn, nil
}

func parseModifiers(modifiers []string) DynamicPolicy {
	var policy DynamicPolicy

	for _, p := range modifiers {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "retry="):
			if val, err := strconv.Atoi(strings.TrimPrefix(p, "retry=")); err == nil && val > 0 {
				policy.RetryCount = val
			}
		case strings.HasPrefix(p, "timeout="):
			if dur, err := time.ParseDuration(strings.TrimPrefix(p, "timeout=")); err == nil && dur > 0 {
				policy.Timeout = dur
			}
		case p == "idempotent":
			policy.Idempotent = true
		case p == "breaker":
			policy.Breaker = true
		}
	}

	return policy
}
