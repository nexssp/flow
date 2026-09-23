package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/directives/builtin/at_schema"
	"github.com/nexssp/flow/dslparse"
	flowtransport "github.com/nexssp/flow/transport"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/validation"
)

func resolveDynamicNode(atom *compiler.AtomExpr, reg *action.Registry, opts *compileOptions) (*action.Builder[any, any], error) {
	act, ok := reg.Get(atom.Name)
	if !ok {
		var available []string
		for _, a := range reg.Actions() {
			if a != nil && a.Describe() != nil {
				available = append(available, a.Describe().Name)
			}
		}
		slices.Sort(available)

		return nil, xerr.NotFound(fmt.Sprintf(
			"PREFLIGHT CHECK FAILED: capability %q does not exist in registry.\n"+
				"👉 Available capabilities (%d): %v",
			atom.Name, len(available), available,
		))
	}

	lineFragment := atom.Name
	if len(atom.Modifiers) > 0 {
		lineFragment += ":" + strings.Join(atom.Modifiers, ":")
	}
	parsedLine, _ := dslparse.ParseLine(lineFragment)
	mod := parsedLine.Modifiers

	resolved, err := resolveService(atom.Name, mod, act)
	if err != nil {
		return nil, xerr.BadRequest(fmt.Sprintf(
			"%s: service resolver rejected modifiers: %v", atom.Name, err))
	}
	act = resolved

	dyn := action.Dynamic(act)

	if mod.CustomName != "" {
		dyn.Name(mod.CustomName)
	} else {
		dyn.Name(atom.Name)
	}
	if mod.Description != "" {
		dyn.Description(mod.Description)
	}
	if mod.SuccessStatus > 0 {
		dyn.SuccessStatus(mod.SuccessStatus)
	}

	if mod.Timeout > 0 {
		dyn.Timeout(mod.Timeout)
	}
	if mod.ConcurrencyLimit > 0 {
		dyn.ConcurrencyLimit(mod.ConcurrencyLimit)
	}

	if mod.RetryMax > 0 {
		backoff := ExponentialJitterOr(mod.BackoffBase, mod.BackoffMax)
		if mod.RetryPredicate != "" {
			dyn.RetryIf(mod.RetryMax, backoff, action.AlwaysRetryPredicate)
		} else {
			dyn.Retry(mod.RetryMax, backoff)
		}
	}

	if mod.RateLimitRPS > 0 {
		burst := mod.RateLimitBurst
		if burst <= 0 {
			burst = int(mod.RateLimitRPS)
			if burst < 1 {
				burst = 1
			}
		}
		dyn.RateLimit(mod.RateLimitRPS, burst)
	}

	if mod.Idempotent {
		if mod.IdempotencyHeader != "" {
			dyn.IdempotentWithConfig(action.IdempotencyConfig{
				Enabled:   true,
				KeyHeader: mod.IdempotencyHeader,
			})
		} else {
			dyn.Idempotent()
		}
	}

	if mod.Schema != "" && opts != nil && opts.schemas != nil {
		if schema, declared := opts.schemas[mod.Schema]; declared {
			dyn.HookBefore(func(ctx context.Context, req any, _ *action.Meta) (context.Context, error) {
				if err := at_schema.Validate(schema, req); err != nil {
					return ctx, err
				}
				return ctx, nil
			})
		}
	}

	hashFn := func(req any) string {
		if req == nil {
			return atom.Name + ":<nil>"
		}
		b, err := json.Marshal(req)
		if err != nil {
			return atom.Name + ":err"
		}
		h := sha256.Sum256(b)

		var buf [sha256.Size * 2]byte
		hex.Encode(buf[:], h[:])

		return atom.Name + ":" + string(buf[:])
	}

	if mod.CacheTTL > 0 {
		dyn.Cache(mod.CacheTTL, hashFn)
	}

	for _, rawMod := range atom.Modifiers {
		switch strings.ToLower(strings.TrimSpace(rawMod)) {
		case "coalesce":
			dyn.Coalesce(action.NewCoalescer(), hashFn)
		case "dedup":
			dyn.Dedup(hashFn)
		case "debug":
			dyn.LogCalls(slog.Default())
		case "validate":
			dyn.Validate(func(ctx context.Context, req any) error {
				if req != nil {
					return validation.Struct(ctx, req)
				}
				return nil
			})
		}
	}

	for i := range mod.Transports {
		binding := &mod.Transports[i]
		resolvedBinding, handled, err := flowtransport.Resolve(binding)
		if err != nil {
			return nil, err
		}
		if !handled {
			return nil, xerr.BadRequest(fmt.Sprintf(
				"%s: transport modifier :%s= is not registered — "+
					"did you forget to import the transport library? "+
					"known kinds: %v",
				atom.Name, binding.Kind, flowtransport.KnownKinds(),
			))
		}
		dyn.Route(resolvedBinding)
	}

	for _, role := range mod.Roles {
		dyn.RequireRole(role)
	}
	for _, perm := range mod.Permissions {
		dyn.RequirePermission(perm)
	}
	for _, feat := range mod.Features {
		dyn.RequireFeature(feat)
	}
	if mod.RequiresAuth {
		dyn.RequireAuth()
	}

	for _, hookName := range mod.HookNames {
		hook, ok := action.NamedHook(hookName)
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf(
				"%s: hook %q is not registered (see action.NamedHookNames() for the catalog)",
				atom.Name, hookName,
			))
		}
		dyn.AnyHook(hook)
	}

	hasInjections := len(atom.Params) > 0 ||
		len(atom.Inputs) > 0 ||
		len(atom.Args) > 0 ||
		atom.Prompt != "" ||
		len(atom.Targets) > 0 ||
		len(atom.Excludes) > 0 ||
		atom.Profile != "" ||
		mod.Provider != "" ||
		mod.Model != "" ||
		mod.System != "" ||
		mod.Image != "" ||
		mod.Typed != "" ||
		mod.Schema != "" ||
		len(mod.Skills) > 0 ||
		len(mod.Tools) > 0 ||
		mod.MaxTurns > 0

	if hasInjections {
		dyn.HookBefore(func(ctx context.Context, req any, _ *action.Meta) (context.Context, error) {
			m, ok := req.(map[string]any)
			if !ok {
				return ctx, nil
			}

			preInjection := make(map[string]any, len(m))
			for kk, vv := range m {
				preInjection[kk] = vv
			}

			for k, v := range atom.Params {
				m[k] = v
			}
			for k, v := range atom.Inputs {
				if val, found := lookupMapPath(m, v); found {
					m[k] = val
				} else {
					m[k] = v
				}
			}

			for k, v := range atom.Args {
				if v.IsWholeStateRef() {
					m[k] = preInjection
					continue
				}
				resolvedVal, rerr := resolveValue(v, m)
				if rerr != nil {
					return ctx, fmt.Errorf("@{...} arg %q: %w", k, rerr)
				}
				m[k] = resolvedVal
			}

			if atom.Prompt != "" {
				m["prompt"] = atom.Prompt
			}
			if len(atom.Targets) > 0 {
				m["targets"] = atom.Targets
			}
			if len(atom.Excludes) > 0 {
				m["excludes"] = atom.Excludes
			}
			if atom.Profile != "" {
				m["profile"] = atom.Profile
			}
			if mod.Provider != "" {
				m["provider"] = mod.Provider
			}
			if mod.Model != "" {
				m["model"] = mod.Model
			}
			if mod.System != "" {
				m["system"] = mod.System
			}
			if mod.Image != "" {
				m["image"] = mod.Image
			}
			if mod.Typed != "" {
				m["typed"] = mod.Typed
			}
			if mod.Schema != "" {
				m["schema"] = mod.Schema
				if mod.Typed == "" {
					m["typed"] = mod.Schema
				}
			}
			if len(mod.Skills) > 0 {
				m["skills"] = mod.Skills
			}
			if len(mod.Tools) > 0 {
				m["tools"] = mod.Tools
			}
			if mod.MaxTurns > 0 {
				m["max_turns"] = mod.MaxTurns
			}

			return ctx, nil
		})
	}

	if opts != nil && len(opts.atomAdvisors) > 0 {
		for _, advisor := range opts.atomAdvisors {
			if advisor != nil {
				advisor(atom.Name, dyn, atom.Modifiers)
			}
		}
	}

	return dyn, nil
}

func lookupMapPath(m map[string]any, path string) (any, bool) {
	if path == "" {
		return m, true
	}

	var cur any = m
	remaining := path

	for remaining != "" {
		var part string

		if dot := strings.IndexByte(remaining, '.'); dot >= 0 {
			part = remaining[:dot]
			remaining = remaining[dot+1:]
		} else {
			part = remaining
			remaining = ""
		}

		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func ExponentialJitterOr(base, maxDelay time.Duration) func(attempt int) time.Duration {
	if base <= 0 {
		base = 20 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 500 * time.Millisecond
	}
	return action.ExponentialJitter(base, maxDelay)
}

func resolveValue(v *compiler.Value, state map[string]any) (any, error) {
	switch v.Kind {
	case compiler.ValueString:
		return v.Str, nil
	case compiler.ValueNumber:
		return v.Num, nil
	case compiler.ValueBool:
		return v.Bool, nil
	case compiler.ValueNull:
		return nil, nil
	case compiler.ValueRef:
		if v.Ref == "" {
			return state, nil
		}
		val, found := lookupMapPath(state, v.Ref)
		if !found {
			return nil, fmt.Errorf("reference .%s not found in state", v.Ref)
		}
		return val, nil
	case compiler.ValueMap:
		out := make(map[string]any, len(v.Map))
		for _, e := range v.Map {
			rv, err := resolveValue(e.Value, state)
			if err != nil {
				return nil, err
			}
			out[e.Key] = rv
		}
		return out, nil
	case compiler.ValueSlice:
		out := make([]any, len(v.Slice))
		for i, e := range v.Slice {
			rv, err := resolveValue(e, state)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return nil, fmt.Errorf("internal: unknown value kind %d", v.Kind)
	}
}
