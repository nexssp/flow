package runner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/extensions/selftestkit"
	"github.com/nexssp/flow/native"
)

func TestExecute_DispatchSeesHooks(t *testing.T) {
	bundles := append(native.Bundles(), selftestkit.Bundle(nil))
	cfg, err := BuildConfig(bundles)
	if err != nil {
		t.Fatal(err)
	}

	var fired atomic.Int32
	cfg.Hooks = []action.AnyHook{{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			fired.Add(1)
			return ctx, nil
		},
	}}

	_, err = Execute(
		context.Background(),
		cfg,
		`dispatch.run @{ members: [cov.echo], payload: { value: 42 } }`,
		"dispatch_test.nflow",
		nil,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fired.Load() == 0 {
		t.Fatal("hooks never fired — registry probably nil")
	}
}
