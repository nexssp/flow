package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/ops/dsl"
	"github.com/nexssp/transport/codec"
	"github.com/nexssp/transport/tcli"
	"github.com/nexssp/transport/thttp"
	"github.com/nexssp/transportai/ta2a"
	"github.com/nexssp/validation"
)

func resolveDynamicNode(atom *compiler.AtomExpr, reg Registry) (*action.Builder[any, any], error) {
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
			"PREFLIGHT CHECK FAILED: capability %q does not exist in registry.\n👉 Available capabilities (%d): %v",
			atom.Name, len(available), available,
		))
	}

	dyn := action.Dynamic(act)

	// Build line fragment and parse using canonical ops/dsl parser
	lineFragment := atom.Name
	if len(atom.Modifiers) > 0 {
		lineFragment += ":" + strings.Join(atom.Modifiers, ":")
	}

	parsedLine, _ := dsl.ParseLine(lineFragment)
	mod := parsedLine.Modifiers

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

	if mod.Path != "" {
		method := strings.ToUpper(mod.Method)
		if method == "" {
			method = "POST"
		}

		newRoute := thttp.HTTPRoute{Method: method, Path: mod.Path}

		for _, b := range act.GetBindings() {
			if r, ok := b.(thttp.HTTPRoute); ok {
				if r.Path != newRoute.Path || r.Method != newRoute.Method {
					return nil, xerr.Conflict(fmt.Sprintf(
						"flow: ambiguous route for %q: Go defines (%s %s), but .flow defines (%s %s). Define it in only one place!",
						atom.Name, r.Method, r.Path, newRoute.Method, newRoute.Path,
					))
				}
			}
		}

		dyn.Route(newRoute)
	}

	for i := range mod.Transports {
		t := &mod.Transports[i]
		switch t.Kind {
		case "cli":
			dyn.Route(tcli.Command(t.Target, mod.CLIDesc).WithAliases(t.Aliases...))
		case "a2a":
			dyn.Route(ta2a.Role(t.Target).WithDescription(mod.A2ADesc))
		}
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

	hashFn := func(req any) string {
		if req == nil {
			return atom.Name + ":<nil>"
		}

		b, err := codec.Default.Marshal(req)
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

	// Check boolean modifier flags
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

	hasInjections := len(atom.Params) > 0 || len(atom.Inputs) > 0 ||
		atom.Prompt != "" || len(atom.Targets) > 0 ||
		len(atom.Excludes) > 0 || atom.Profile != ""

	if hasInjections {
		dyn.HookBefore(func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			if m, ok := req.(map[string]any); ok {
				for k, v := range atom.Params {
					m[k] = v
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
			}

			return ctx, nil
		})
	}

	return dyn, nil
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
