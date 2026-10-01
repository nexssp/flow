package core

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/validation"
)

// strictKey marks a compilation context as requiring runtime request
// validation on every typed atom. It is a compilation-scoped flag, not
// a process-scoped one: two .nflow files loaded by the same binary may
// choose different strictness.
var strictKey = xctx.NewKey[bool]("flow.strict")

// WithStrict returns ctx marked for strict validation.
func WithStrict(ctx context.Context) context.Context {
	return strictKey.With(ctx, true)
}

// strictFromCtx reports whether strict mode is active. xctx handles a
// nil context.
func strictFromCtx(ctx context.Context) bool {
	v, _ := strictKey.From(ctx)
	return v
}

// isStrictConfig reports whether meta["config"]["strict"] resolves to true.
// Called from CompileAction after Preprocess.
func isStrictConfig(meta map[string]any) bool {
	cfg, _ := meta["config"].(map[string]string)
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg["strict"]), "true")
}

// hasModifier reports whether an atom's raw modifier list carries name.
// Comparison is case-insensitive and ignores any `=value` suffix.
func hasModifier(mods []string, name string) bool {
	for _, raw := range mods {
		if i := strings.IndexByte(raw, '='); i >= 0 {
			raw = raw[:i]
		}
		if strings.EqualFold(strings.TrimSpace(raw), name) {
			return true
		}
	}
	return false
}

// strictHook returns the validation hook strict mode installs on each
// typed atom. Indirection kept so a future policy change lands in one
// place.
func strictHook() action.AnyHook {
	return validation.Hook()
}
