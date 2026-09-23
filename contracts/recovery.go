package contracts

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

// RecoveredError carries the failing error through the recovery
// target's context. Recovery targets that care about the cause read it
// with RecoveredErrorFrom. Targets that only need the original input
// ignore it.
type RecoveredError struct {
	Err       error
	Kind      string // string form of xerr.Kind; empty for non-xerr errors
	Suspended bool
}

var recoveredErrorKey = xctx.NewKey[*RecoveredError]("flow.recovery.error")

// WithRecoveredError installs r into ctx. Called by a recovery node
// before it invokes the matched target.
func WithRecoveredError(ctx context.Context, r *RecoveredError) context.Context {
	if r == nil {
		return ctx
	}
	return recoveredErrorKey.With(ctx, r)
}

// RecoveredErrorFrom returns the error that triggered recovery, if any.
func RecoveredErrorFrom(ctx context.Context) (*RecoveredError, bool) {
	return recoveredErrorKey.From(ctx)
}
