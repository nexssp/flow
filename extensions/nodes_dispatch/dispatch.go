package nodes_dispatch

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/contracts"
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
	if len(req.Members) == 0 {
		return nil, xerr.NotFound("dispatch: no member succeeded")
	}

	var lastErr error
	candidates := make([]action.AnyAction, 0, len(req.Members))
	for _, name := range req.Members {
		member := name
		candidates = append(candidates, action.New("dispatch.member", func(_ context.Context, payload any) (any, error) {
			// Resolve only when this candidate is reached, preserving the legacy
			// lookup/invocation order and avoiding lookups after a successful member.
			target, ok := resolver.Action(member)
			if !ok {
				lastErr = xerr.NotFound("dispatch: member not registered: " + member)
				return nil, lastErr
			}

			output, err := action.InvokeAny(ctx, target, payload)
			if err != nil {
				lastErr = err
			}
			return output, err
		}).Build())
	}

	firstSuccess := action.FirstSuccessAny("dispatch.run", candidates...).Build()
	// Candidates use the original context. Detaching only the coordinator keeps
	// Kernel's cancellation short-circuit from changing dispatch's existing policy.
	output, err := action.InvokeAny(context.WithoutCancel(ctx), firstSuccess, req.Payload)
	if err != nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, err
	}
	return output, nil
}).Description("Tries each explicitly listed action in order until one succeeds").
	Tag("dispatch", "routing").
	Build()
