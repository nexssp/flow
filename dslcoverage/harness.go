// Package coverage holds the coverage test suite for the flow DSL.
//
// The harness actions here are named `cov.*` so they cannot collide
// with real actions. Each one is deliberately tiny and deterministic.
package coverage

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/dslparse"
	flowtransport "github.com/nexssp/flow/transport"
)

var covFlakyCount atomic.Int32

func coverageActions() []action.AnyAction {
	return []action.AnyAction{
		covEcho(),
		covValue(),
		covSlow("cov.slow", 100*time.Millisecond),
		covSlow("cov.fast", 10*time.Millisecond),
		covFlaky(2),
		covFailKind("Timeout"),
		covRequireRole("admin"),
		covRequirePerm("write"),
		covBoom(),
		covRecoverable(),
	}
}

func covEcho() action.AnyAction {
	return action.New("cov.echo", func(_ context.Context, in map[string]any) (map[string]any, error) {
		out := map[string]any{}
		for k, v := range in {
			out[k] = v
		}
		return out, nil
	}).Build()
}

func covValue() action.AnyAction {
	return action.New("cov.value", func(_ context.Context, in map[string]any) (map[string]any, error) {
		n, _ := in["n"].(int)
		if n == 0 {
			if f, ok := in["n"].(float64); ok {
				n = int(f)
			}
		}
		return map[string]any{"n": n, "value": n * 2}, nil
	}).Build()
}

func covSlow(name string, d time.Duration) action.AnyAction {
	return action.New(name, func(_ context.Context, in map[string]any) (map[string]any, error) {
		time.Sleep(d)
		out := map[string]any{}
		for k, v := range in {
			out[k] = v
		}
		out["from"] = name
		return out, nil
	}).Build()
}

func covFlaky(n int32) action.AnyAction {
	return action.New("cov.flaky", func(_ context.Context, in map[string]any) (map[string]any, error) {
		got := covFlakyCount.Add(1)
		if got <= n {
			return nil, xerr.Unavailable("simulated transient failure")
		}
		out := map[string]any{"attempt": int(got)}
		for k, v := range in {
			out[k] = v
		}
		return out, nil
	}).Build()
}

func covFailKind(kind string) action.AnyAction {
	return action.New("cov.fail."+kind, func(_ context.Context, in map[string]any) (map[string]any, error) {
		switch kind {
		case "Timeout":
			return in, xerr.Timeout("simulated timeout")
		default:
			return in, xerr.Internal("simulated failure")
		}
	}).Build()
}

func covRequireRole(role string) action.AnyAction {
	return action.New("cov.require."+role, func(_ context.Context, in map[string]any) (map[string]any, error) {
		return in, nil
	}).Build()
}

func covRequirePerm(perm string) action.AnyAction {
	return action.New("cov.require.perm."+perm, func(_ context.Context, in map[string]any) (map[string]any, error) {
		return in, nil
	}).Build()
}

func covBoom() action.AnyAction {
	return action.New("cov.boom", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, xerr.Internal("boom")
	}).Build()
}

func covRecoverable() action.AnyAction {
	return action.New("cov.recoverable",
		func(_ context.Context, _ any) (string, error) {
			return "recoverable", nil
		}).Build()
}

// TestContext returns a context seeded with privileges for modifier coverage tests.
func TestContext() context.Context {
	ctx := context.Background()
	ctx = xctx.WithRoles(ctx, []string{"admin"})
	ctx = xctx.WithPermissions(ctx, []string{"write"})
	ctx = xctx.WithFeatures(ctx, []string{"coverage_enabled"})
	ctx = xctx.WithApprovalToken(ctx, "coverage_approval,all")
	return ctx
}

func init() {
	// Stub resolvers for DSL syntax and modifier coverage tests.
	// This allows flow to verify transport modifiers without depending on external transport modules.
	flowtransport.RegisterResolverMap(map[string]flowtransport.ResolverFunc{
		"cli": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:cli", nil
		},
		"http": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:http", nil
		},
		"raw": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:raw", nil
		},
		"sse": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:sse", nil
		},
		"nats-pubsub": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:nats-pubsub", nil
		},
		"nats-rpc": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:nats-rpc", nil
		},
		"nats-kv": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:nats-kv", nil
		},
		"nats-durable": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:nats-durable", nil
		},
		"nats-consumer": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:nats-consumer", nil
		},
		"topic": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:topic", nil
		},
		"cron": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:cron", nil
		},
		"worker": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:worker", nil
		},
		"a2a": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:a2a", nil
		},
		"mcp": func(b *dslparse.TransportBinding) (action.Binding, error) {
			return "stub:mcp", nil
		},
	})
}
