package pool

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/core"
)

// materialize mounts one `pool.<name>` action per declaration. The
// action's shape depends on the declared strategy.
func materialize(req core.MaterializeReq) error {
	for _, declaration := range DeclarationsFromMeta(req.Meta) {
		act, err := buildPoolAction(declaration, req.Resolver)
		if err != nil {
			return xerr.BadRequest("@pool "+declaration.Name+": "+err.Error(), err)
		}
		if err := req.Resolver.Mount(action.Library{
			Name:    "pool." + declaration.Name,
			Actions: []action.AnyAction{act},
		}); err != nil {
			return xerr.Internal("@pool "+declaration.Name+": mount: "+err.Error(), err)
		}
	}
	return nil
}

// wrapWithPools publishes the pools table into the context so that
// dispatch can resolve pool names at runtime. A pipeline without any
// @pool declaration returns the inner action untouched.
func wrapWithPools(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
	pools := PoolsFromMeta(meta)
	if len(pools) == 0 {
		return inner, nil
	}
	return action.New("pool.wrap", func(ctx context.Context, req any) (any, error) {
		return action.InvokeAny(contracts.WithPools(ctx, pools), inner, req)
	}).Build(), nil
}
