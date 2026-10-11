package contracts

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

type RecoveredError struct {
	Err       error
	Kind      string
	Suspended bool
}

var recoveredErrorKey = xctx.NewKey[*RecoveredError]("flow.recovery.error")

func WithRecoveredError(ctx context.Context, r *RecoveredError) context.Context {
	if r == nil {
		return ctx
	}
	return recoveredErrorKey.With(ctx, r)
}

func RecoveredErrorFrom(ctx context.Context) (*RecoveredError, bool) {
	return recoveredErrorKey.From(ctx)
}
