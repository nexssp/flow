package nodes

import (
	"context"
	"strings"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// DispatchReq is the request shape for the dispatch node.
//
// Members is a comma-separated list of action names, or empty when
// Pool is set. Pool names a @pool declaration, which the node expands
// into Members by reading the pool table from the execution context.
//
// Chosen is the name the upstream node or LLM selected. Fallback is
// the name used when Chosen is missing or fails. Payload is what every
// candidate is invoked with.
type DispatchReq struct {
	Pool     string `json:"pool,omitempty"`
	Members  string `json:"members,omitempty"`
	Chosen   string `json:"chosen,omitempty"`
	Fallback string `json:"fallback,omitempty"`
	Payload  any    `json:"payload,omitempty"`
}

// NewDispatchAction returns the dispatch node.
//
// Dispatch order:
//
//  1. Chosen, when it names a member.
//  2. Fallback, when Chosen is empty or unknown.
//  3. Every member in declaration order, first success wins.
//
// The node fails only when every candidate fails and no fallback
// succeeded; the returned error wraps the last failure so the caller
// sees the actual cause, not a generic "dispatch failed".
//
// Resolution of pool → members happens at call time, not at compile
// time, because the pool table is only known to the runner.
func NewDispatchAction() action.AnyAction {
	return action.New("dispatch", func(ctx context.Context, req DispatchReq) (any, error) {
		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return nil, xerr.Internal("dispatch: no registry in context")
		}

		// Expand pool → members. Members wins if both are set, which
		// lets a caller bypass the pool table without editing the DSL.
		if req.Members == "" && req.Pool != "" {
			pools := contracts.PoolsFromContext(ctx)
			if pools == nil {
				return nil, xerr.Internal("dispatch: pool table not available in context")
			}
			members, ok := pools[req.Pool]
			if !ok {
				return nil, xerr.NotFound("dispatch: pool not declared: " + req.Pool)
			}
			req.Members = strings.Join(members, ",")
		}

		members := splitMembers(req.Members)
		if len(members) == 0 {
			return nil, xerr.BadRequest("dispatch: members is empty")
		}

		// 1. Chosen, when valid.
		if req.Chosen != "" {
			if act, ok := reg.Get(req.Chosen); ok {
				out, err := action.InvokeAny(ctx, act, req.Payload)
				if err == nil {
					return out, nil
				}
			}
		}

		// 2. Fallback, when named.
		if req.Fallback != "" {
			if act, ok := reg.Get(req.Fallback); ok {
				out, err := action.InvokeAny(ctx, act, req.Payload)
				if err == nil {
					return out, nil
				}
			}
		}

		// 3. Every member, first success wins.
		var lastErr error

		for _, name := range members {
			act, ok := reg.Get(name)
			if !ok {
				lastErr = xerr.NotFound("dispatch: member not registered: " + name)
				continue
			}
			out, err := action.InvokeAny(ctx, act, req.Payload)
			if err == nil {
				return out, nil
			}
			lastErr = err
		}

		if lastErr == nil {
			lastErr = xerr.NotFound("dispatch: no member succeeded")
		}
		return nil, lastErr
	}).
		Description("Selects one action from a named pool and invokes it, with fallback").
		Tag("dispatch", "pool", "routing").
		Build()
}

// splitMembers splits a comma-separated member list, trimming
// whitespace and dropping empty entries.
func splitMembers(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
