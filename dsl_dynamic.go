package flow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type DynamicPolicy struct {
	RetryCount int
	Timeout    time.Duration
	Idempotent bool
	Breaker    bool
}

// resolveDynamicNode parses inline node policy flags e.g. "payment.charge:retry=3:timeout=200ms:idempotent"
func resolveDynamicNode(expr string, reg Registry) (*action.Builder[any, any], error) {
	cleanExpr, policy := parseNodePolicies(expr)
	name, params := parseTokenParams(cleanExpr)

	act, ok := reg.Get(name)
	if !ok {
		return nil, xerr.NotFound(fmt.Sprintf("flow: capability %q not found in registry", name))
	}

	dyn := action.Dynamic(act)

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

	if len(params) > 0 {
		dyn.HookBefore(func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			if m, ok := req.(map[string]any); ok {
				for k, v := range params {
					m[k] = v
				}
			}
			return ctx, nil
		})
	}

	return dyn, nil
}

func parseNodePolicies(token string) (string, DynamicPolicy) {
	parts := strings.Split(token, ":")
	if len(parts) == 1 {
		return token, DynamicPolicy{}
	}

	cleanParts := make([]string, 0, len(parts))
	cleanParts = append(cleanParts, parts[0])
	var policy DynamicPolicy

	for i := 1; i < len(parts); i++ {
		p := strings.TrimSpace(parts[i])
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
		default:
			// Retain non-policy modifier flags (e.g., profile:arch) for parseTokenParams
			cleanParts = append(cleanParts, p)
		}
	}

	return strings.Join(cleanParts, ":"), policy
}
