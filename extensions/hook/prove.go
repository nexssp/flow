package hook

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

// HookProbeKey is the context key set by the hook.verify hook. The
// probe action reads it; fixtures assert on the result.
var HookProbeKey = xctx.NewKey[bool]("flow.hook.probe")

// ProbeRes is the typed response. Keeping a concrete struct avoids
// per-call map allocations on the hot path.
type ProbeRes struct {
	Payload      any  `json:"payload,omitempty"`
	HookVerified bool `json:"hook_verified"`
}

// ProbeAction reads HookProbeKey from the context and returns it as
// part of the typed result. Used by fixtures to prove a hook ran.
func ProbeAction() action.AnyAction {
	return action.New("hook.probe", func(ctx context.Context, in any) (ProbeRes, error) {
		active, _ := HookProbeKey.From(ctx)
		return ProbeRes{Payload: in, HookVerified: active}, nil
	}).
		Description("Report whether a @hook altered the context").
		Build()
}
