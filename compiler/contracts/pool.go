package contracts

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

var poolsKey = xctx.NewKey[map[string][]string]("flow.pools")

func WithPools(ctx context.Context, pools map[string][]string) context.Context {
	if len(pools) == 0 {
		return ctx
	}
	return poolsKey.With(ctx, pools)
}

func PoolsFromContext(ctx context.Context) map[string][]string {
	p, _ := poolsKey.From(ctx)
	return p
}
