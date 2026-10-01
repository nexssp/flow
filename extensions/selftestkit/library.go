// Package selftestkit ships the coverage fixtures the self-test suite
// references as `cov.*`. It is linked only into the selftest runner,
// so its actions never pollute the production resolver.
package selftestkit

import (
	"context"
	"iter"
	"maps"
	"sync/atomic"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

const ID = "selftestkit"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{library()},
	}
}

func library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			covEcho(), covDouble(), covFast(), covSlow(), covFlaky(),
			covBoom(), covRecoverable(), covFailTimeout(), covFailUnavailable(),
			covStubAction(),
		},
		Sources: []action.AnyStreamAction{covItems()},
	}
}

func covEcho() action.AnyAction {
	return action.New("cov.echo", func(_ context.Context, in map[string]any) (map[string]any, error) {
		out := make(map[string]any, len(in))
		maps.Copy(out, in)
		return out, nil
	}).Build()
}

func covDouble() action.AnyAction {
	return action.New("cov.double", func(_ context.Context, in map[string]any) (map[string]any, error) {
		n := 0
		switch v := in["n"].(type) {
		case int:
			n = v
		case int64:
			n = int(v)
		case float64:
			n = int(v)
		}
		out := make(map[string]any, len(in)+1)
		maps.Copy(out, in)
		out["value"] = n * 2
		return out, nil
	}).Build()
}

func covFast() action.AnyAction {
	return action.New("cov.fast", func(_ context.Context, in map[string]any) (map[string]any, error) {
		out := make(map[string]any, len(in)+1)
		maps.Copy(out, in)
		out["from"] = "cov.fast"
		return out, nil
	}).Build()
}

func covSlow() action.AnyAction {
	return action.New("cov.slow", func(_ context.Context, in map[string]any) (map[string]any, error) {
		time.Sleep(50 * time.Millisecond)
		out := make(map[string]any, len(in)+1)
		maps.Copy(out, in)
		out["from"] = "cov.slow"
		return out, nil
	}).Build()
}

var covFlakyCount atomic.Int32

func covFlaky() action.AnyAction {
	return action.New("cov.flaky", func(_ context.Context, in map[string]any) (map[string]any, error) {
		if covFlakyCount.Add(1) <= 2 {
			return nil, xerr.Unavailable("simulated transient")
		}
		out := make(map[string]any, len(in)+1)
		maps.Copy(out, in)
		out["ok"] = true
		return out, nil
	}).Build()
}

func covBoom() action.AnyAction {
	return action.New("cov.boom", func(_ context.Context, _ any) (any, error) {
		return nil, xerr.Internal("boom")
	}).Build()
}

func covRecoverable() action.AnyAction {
	return action.New("cov.recoverable", func(_ context.Context, _ any) (string, error) {
		return "recoverable", nil
	}).Build()
}

func covFailTimeout() action.AnyAction {
	return action.New("cov.fail.timeout", func(_ context.Context, _ any) (any, error) {
		return nil, xerr.Timeout("simulated timeout")
	}).Build()
}

func covFailUnavailable() action.AnyAction {
	return action.New("cov.fail.unavailable", func(_ context.Context, _ any) (any, error) {
		return nil, xerr.Unavailable("simulated unavailable")
	}).Build()
}

func covStubAction() action.AnyAction {
	return action.New("cov.stub", func(_ context.Context, in any) (any, error) {
		return in, nil
	}).Build()
}

func covItems() action.AnyStreamAction {
	return action.NewStream("cov.items", func(_ context.Context, _ struct{}) (iter.Seq2[string, error], error) {
		return func(yield func(string, error) bool) {
			for _, item := range []string{"item_1", "item_2", "item_3"} {
				if !yield(item, nil) {
					return
				}
			}
		}, nil
	})
}
