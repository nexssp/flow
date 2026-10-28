// Package selftestkit ships the coverage fixtures the self-test suite
// references as `cov.*`. It is linked only into the selftest runner,
// so its actions never pollute the production resolver.
package selftestkit

import (
	"context"
	"embed"
	"iter"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/contracts"
)

const ID = "selftestkit"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:              ID,
		Libraries:       []action.Library{library()},
		Fixtures:        fixturesFS,
		SourceModules:   fixturesFS,
		ExportedModules: []string{"nflows/bundle_source--module.nflow"},
		SelfTest:        bundleSourceErrorSelfTests,
	}
}

func bundleSourceErrorSelfTests() []compiler.SelfTestSection {
	cases := []struct {
		name, file, want string
	}{
		{"private source modules are rejected", "nflows/bundle_source--private-test.nflow", "private or not exported"},
		{"unknown bundle qualifiers are rejected", "nflows/bundle_source--unknown.nflow", "is not loaded"},
	}
	features := make([]compiler.SelfTestFeature, 0, len(cases))
	for _, tc := range cases {
		source, err := fixturesFS.ReadFile(tc.file)
		if err != nil {
			source = []byte("@include selftestkit:missing.nflow AS missing")
		}
		features = append(features, compiler.SelfTestFeature{
			Name:               tc.name,
			DSL:                string(source),
			ExpectCompileError: tc.want,
			Source:             "selftestkit/" + tc.file,
		})
	}
	return []compiler.SelfTestSection{{Name: ID, Features: features}}
}

func library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			covEcho(), covDouble(), covFast(), covSlow(), covFlaky(),
			covBoom(), covRecoverable(), covFailTimeout(), covFailUnavailable(),
			covStubAction(), covReset(), covIncrement(), covCount(),
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

// Self-test fixtures run sequentially, so reset occurs only between pipelines.
// Atomic increments and reads keep concurrent service actions race-free.
var covCounter atomic.Int64

var covIncrementStarted = struct {
	sync.Mutex
	channel chan struct{}
	closed  bool
}{channel: make(chan struct{})}

func covReset() action.AnyAction {
	return action.New("cov.reset", func(_ context.Context, in any) (any, error) {
		covCounter.Store(0)
		covIncrementStarted.Lock()
		covIncrementStarted.channel = make(chan struct{})
		covIncrementStarted.closed = false
		covIncrementStarted.Unlock()
		return in, nil
	}).Description("Reset the self-test service counter").Build()
}

func covIncrement() action.AnyAction {
	return action.New("cov.increment", func(ctx context.Context, in any) (any, error) {
		if covCounter.Add(1) == 1 {
			covIncrementStarted.Lock()
			if !covIncrementStarted.closed {
				close(covIncrementStarted.channel)
				covIncrementStarted.closed = true
			}
			covIncrementStarted.Unlock()
		}
		contracts.SignalReady(ctx)
		<-ctx.Done()
		return in, ctx.Err()
	}).Description("Count, signal readiness, and wait for cancellation").Build()
}

func covCount() action.AnyAction {
	return action.New("cov.count", func(_ context.Context, _ any) (int64, error) {
		return covCounter.Load(), nil
	}).Description("Read the self-test service counter").Build()
}

func covBoom() action.AnyAction {
	return action.New("cov.boom", func(ctx context.Context, in any) (any, error) {
		if options, ok := in.(map[string]any); ok && options["wait_for_increment"] == true {
			covIncrementStarted.Lock()
			started := covIncrementStarted.channel
			covIncrementStarted.Unlock()
			select {
			case <-started:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
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
