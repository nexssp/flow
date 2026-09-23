package nodes_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

// ─── explicit members ──────────────────────────────────────────────────

func TestDispatch_ChosenIsUsedFirst(t *testing.T) {
	reg := action.MustNewRegistry(action.Of(
		action.New("fast", func(_ context.Context, in string) (string, error) {
			return "fast:" + in, nil
		}).Build(),
		action.New("slow", func(_ context.Context, in string) (string, error) {
			return "slow:" + in, nil
		}).Build(),
	))

	res := mustInvokeReg(t, reg, nodes.NewDispatchAction(), nodes.DispatchReq{
		Members: "fast, slow",
		Chosen:  "slow",
		Payload: "x",
	})
	if res != "slow:x" {
		t.Fatalf("got %v, want slow:x", res)
	}
}

func TestDispatch_FallbackWhenChosenMissing(t *testing.T) {
	reg := action.MustNewRegistry(action.Of(
		action.New("fast", func(_ context.Context, in string) (string, error) {
			return "fast:" + in, nil
		}).Build(),
		action.New("local", func(_ context.Context, in string) (string, error) {
			return "local:" + in, nil
		}).Build(),
	))

	res := mustInvokeReg(t, reg, nodes.NewDispatchAction(), nodes.DispatchReq{
		Members:  "fast, local",
		Chosen:   "nonexistent",
		Fallback: "local",
		Payload:  "x",
	})
	if res != "local:x" {
		t.Fatalf("got %v, want local:x", res)
	}
}

func TestDispatch_FallsThroughToMembersOnError(t *testing.T) {
	reg := action.MustNewRegistry(action.Of(
		action.New("flaky", func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("flaky failed")
		}).Build(),
		action.New("stable", func(_ context.Context, in string) (string, error) {
			return "stable:" + in, nil
		}).Build(),
	))

	res := mustInvokeReg(t, reg, nodes.NewDispatchAction(), nodes.DispatchReq{
		Members: "flaky, stable",
		Chosen:  "flaky",
		Payload: "x",
	})
	if res != "stable:x" {
		t.Fatalf("got %v, want stable:x", res)
	}
}

func TestDispatch_RejectsEmptyMembers(t *testing.T) {
	_, err := action.InvokeAny(
		ctxWithRegistry(t, action.MustNewRegistry()),
		nodes.NewDispatchAction(),
		nodes.DispatchReq{Members: ""})
	if err == nil || !strings.Contains(err.Error(), "members is empty") {
		t.Fatalf("got %v", err)
	}
}

func TestDispatch_NoRegistry(t *testing.T) {
	_, err := action.InvokeAny(context.Background(), nodes.NewDispatchAction(),
		nodes.DispatchReq{Members: "a"})
	if err == nil || !strings.Contains(err.Error(), "no registry in context") {
		t.Fatalf("got %v", err)
	}
}

// ─── pool-based ────────────────────────────────────────────────────────

func TestDispatch_PoolExpandsToMembers(t *testing.T) {
	reg := action.MustNewRegistry(action.Of(
		action.New("fast", func(_ context.Context, in string) (string, error) {
			return "fast:" + in, nil
		}).Build(),
		action.New("smart", func(_ context.Context, in string) (string, error) {
			return "smart:" + in, nil
		}).Build(),
	))

	pools := map[string][]string{
		"experts": {"fast", "smart"},
	}

	ctx := ctxWithRegistry(t, reg)
	ctx = contracts.WithPools(ctx, pools)

	res, err := action.InvokeAny(ctx, nodes.NewDispatchAction(), nodes.DispatchReq{
		Pool:    "experts",
		Chosen:  "smart",
		Payload: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res != "smart:x" {
		t.Fatalf("got %v, want smart:x", res)
	}
}

func TestDispatch_PoolNotDeclared(t *testing.T) {
	reg := action.MustNewRegistry()
	ctx := ctxWithRegistry(t, reg)
	ctx = contracts.WithPools(ctx, map[string][]string{"experts": {"fast"}})

	_, err := action.InvokeAny(ctx, nodes.NewDispatchAction(), nodes.DispatchReq{
		Pool: "unknown",
	})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("got %v", err)
	}
}

func TestDispatch_MembersOverridesPool(t *testing.T) {
	reg := action.MustNewRegistry(action.Of(
		action.New("fast", func(_ context.Context, _ string) (string, error) {
			return "fast", nil
		}).Build(),
		action.New("only", func(_ context.Context, _ string) (string, error) {
			return "only", nil
		}).Build(),
	))

	ctx := ctxWithRegistry(t, reg)
	ctx = contracts.WithPools(ctx, map[string][]string{"experts": {"fast"}})

	res, err := action.InvokeAny(ctx, nodes.NewDispatchAction(), nodes.DispatchReq{
		Pool:    "experts",
		Members: "only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res != "only" {
		t.Fatalf("Members should win over Pool, got %v", res)
	}
}

// ─── dialect integration ───────────────────────────────────────────────

// TestDispatch_DialectIntegration exercises the full .nflow syntax
// through the preprocessor + compiler + runtime:
//
//	@pool experts [fast, smart]
//	start -> dispatch(pool=experts, chosen=smart)
//
// The registry must contain the dispatch action itself, in addition to
// the pool members and the upstream start action. In production the
// dispatch action comes from flow.StandardLibrary(); here it is added
// by hand.
func TestDispatch_DialectIntegration(t *testing.T) {
	src := `@pool experts [fast, smart]

start
-> dispatch(pool=experts, chosen=smart)
`

	pre, err := flow.PreprocessBytes([]byte(src), "dispatch.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Pools) != 1 || pre.Pools[0].Name != "experts" {
		t.Fatalf("pools = %+v", pre.Pools)
	}

	startAct := action.New("start", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{}, nil
	}).Build()
	fastAct := action.New("fast", func(_ context.Context, _ map[string]any) (string, error) {
		return "fast", nil
	}).Build()
	smartAct := action.New("smart", func(_ context.Context, _ map[string]any) (string, error) {
		return "smart", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(
		startAct,
		fastAct,
		smartAct,
		nodes.NewDispatchAction(),
	))

	built, err := flow.CompilePipeline(pre.DSL, reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	pools := map[string][]string{
		pre.Pools[0].Name: pre.Pools[0].Members,
	}

	// Production runs go through flow/runner, which places the
	// registry into the execution context before calling Do. A test
	// that calls Do directly must do the same.
	ctx := contracts.WithRegistry(context.Background(), reg)
	ctx = contracts.WithPools(ctx, pools)

	out, err := built.Build().Do(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "smart" {
		t.Fatalf("got %v, want smart", out)
	}
}
