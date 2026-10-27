package lang

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

var autoInstallDisabledKey = xctx.NewKey[bool]("flow.lang.auto-install-disabled")

// WithAutoInstallDisabled marks a context so language preflight and runtime
// resolution report missing interpreters without invoking package managers.
// Self-test uses this to avoid changing the host environment.
func WithAutoInstallDisabled(ctx context.Context) context.Context {
	return autoInstallDisabledKey.With(ctx, true)
}

func autoInstallDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := autoInstallDisabledKey.From(ctx)
	return disabled
}
