package contracts

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

var poolsKey = xctx.NewKey[map[string][]string]("flow.pools")

// WithPools attaches the flow's @pool declarations to the execution
// context. The runner calls this before Execute so that a dispatch
// node can look up its pool by name without the compiler having to
// thread the pool table through every layer.
//
// A nil or empty map is a no-op.
func WithPools(ctx context.Context, pools map[string][]string) context.Context {
	if len(pools) == 0 {
		return ctx
	}
	return poolsKey.With(ctx, pools)
}

// PoolsFromContext reads the pool table WithPools stored. Missing key
// returns nil, which the dispatch node treats as "no pools declared".
func PoolsFromContext(ctx context.Context) map[string][]string {
	p, _ := poolsKey.From(ctx)
	return p
}
