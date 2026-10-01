// Package hook provides the `@hook:name` directive, which attaches
// every named hook from the kernel registry to the compiled pipeline,
// and the hook.probe action that reads HookProbeKey from the context
// to prove a hook fired.
//
// The bundle registers the reserved "hook.verify" hook, which sets
// HookProbeKey in the request context. Fixtures use it to confirm that
// @hook actually attached something.
//
// Typical use:
//
//	@hook:hook.verify
//	{ test: "data" } -> hook.probe
package hook

import (
	"context"
	"embed"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

const ID = "hook"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)

	// hook.verify is a reserved name; a second registration is a
	// programming error and MustRegisterHook panics on it.
	action.MustRegisterHook("hook.verify", func() action.AnyHook {
		return action.AnyHook{
			Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
				return HookProbeKey.With(ctx, true), nil
			},
		}
	})
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:           ID,
		Libraries:    []action.Library{{Name: ID, Actions: []action.AnyAction{ProbeAction()}}},
		Directives:   []core.Directive{Directive},
		WrapPipeline: wrapFromMeta,
		Fixtures:     fixturesFS,
	}
}

// wrapFromMeta reads meta["hooks"] and attaches every named hook to
// the compiled program. An unknown hook name is a compile-time error,
// not a runtime no-op.
func wrapFromMeta(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
	names, _ := meta["hooks"].([]string)
	if len(names) == 0 {
		return inner, nil
	}

	hooks := make([]action.AnyHook, 0, len(names))
	for _, name := range names {
		named, found := action.NamedHook(name)
		if !found {
			return nil, xerr.NotFound("@hook:" + name + " is not registered in the kernel")
		}
		hooks = append(hooks, named)
	}
	inner.AddAnyHook(hooks...)
	return inner, nil
}
