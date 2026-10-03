package nodes_dispatch

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

type DispatchReq struct {
	Pool     string   `json:"pool,omitempty"`
	Members  []string `json:"members,omitempty"`
	Chosen   string   `json:"chosen,omitempty"`
	Fallback string   `json:"fallback,omitempty"`
	Payload  any      `json:"payload,omitempty"`
}

var Dispatch = action.New("dispatch.run", func(ctx context.Context, req DispatchReq) (any, error) {
	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return nil, xerr.Internal("dispatch: no action resolver in context")
	}

	names := req.Members
	if len(names) == 0 && req.Pool != "" {
		pools := contracts.PoolsFromContext(ctx)
		if pools == nil {
			return nil, xerr.Internal("dispatch: pool table not available in context")
		}
		listed, ok := pools[req.Pool]
		if !ok {
			return nil, xerr.NotFound("dispatch: pool not declared: " + req.Pool)
		}
		names = listed
	}
	if len(names) == 0 {
		return nil, xerr.BadRequest("dispatch: members is empty")
	}

	if req.Chosen != "" {
		if target, ok := resolver.Action(req.Chosen); ok {
			if output, err := action.InvokeAny(ctx, target, req.Payload); err == nil {
				return output, nil
			}
		}
	}

	if req.Fallback != "" {
		if target, ok := resolver.Action(req.Fallback); ok {
			if output, err := action.InvokeAny(ctx, target, req.Payload); err == nil {
				return output, nil
			}
		}
	}

	var lastErr error
	for _, name := range names {
		target, ok := resolver.Action(name)
		if !ok {
			lastErr = xerr.NotFound("dispatch: member not registered: " + name)
			continue
		}
		output, err := action.InvokeAny(ctx, target, req.Payload)
		if err == nil {
			return output, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = xerr.NotFound("dispatch: no member succeeded")
	}
	return nil, lastErr
}).Description("Selects one action from a pool or list and invokes it").
	Tag("dispatch", "pool", "routing").
	Build()

func splitMembers(csv string) []string {
	if csv == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(csv, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
