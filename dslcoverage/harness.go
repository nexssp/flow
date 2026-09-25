// Package coverage holds the coverage test suite for the flow DSL.
package coverage

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/nexssp/flow/dslparse"
	flowtransport "github.com/nexssp/flow/transport"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
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
		covStubResolver(),
	}
}

func covStubResolver() action.AnyAction {
	return action.New("cov.stub_resolver", func(_ context.Context, binding *dslparse.TransportBinding) (action.Binding, error) {
		return "stub:" + binding.Kind, nil
	}).
		Route(
			flowtransport.OnDSL("cli"),
			flowtransport.OnDSL("http"),
			flowtransport.OnDSL("route"),
			flowtransport.OnDSL("raw"),
			flowtransport.OnDSL("sse"),
			flowtransport.OnDSL("nats"),
			flowtransport.OnDSL("nats-pubsub"),
			flowtransport.OnDSL("nats-rpc"),
			flowtransport.OnDSL("nats-kv"),
			flowtransport.OnDSL("nats-durable"),
			flowtransport.OnDSL("nats-consumer"),
			flowtransport.OnDSL("topic"),
			flowtransport.OnDSL("cron"),
			flowtransport.OnDSL("worker"),
			flowtransport.OnDSL("a2a"),
			flowtransport.OnDSL("mcp"),
		).
		Build()
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

func TestContext() context.Context {
	ctx := context.Background()
	ctx = xctx.WithRoles(ctx, []string{"admin"})
	ctx = xctx.WithPermissions(ctx, []string{"write"})
	ctx = xctx.WithFeatures(ctx, []string{"coverage_enabled"})
	ctx = xctx.WithApprovalToken(ctx, "coverage_approval,all")
	return ctx
}
