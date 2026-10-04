package nodes_dispatch

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

type DispatchReq struct {
	Members []string `json:"members"`
	Payload any      `json:"payload"`
}

var Dispatch = action.New("dispatch.run", func(ctx context.Context, req DispatchReq) (any, error) {
	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return nil, xerr.Internal("dispatch: no action resolver in context")
	}

	var lastErr error
	for _, name := range req.Members {
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
}).Description("Tries each explicitly listed action in order until one succeeds").
	Tag("dispatch", "routing").
	Build()
