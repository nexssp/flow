// path: nexssp/flow/nodes/bench_test.go
//
// Filesystem-touching tests use t.Chdir(tmp) + relative paths so the
// xfs.Rel path guard accepts the input. This matches the production
// contract: bench.save / bench.compare are invoked from a workspace
// directory, not with absolute paths.
//
// t.Chdir forbids t.Parallel(); tests that mutate CWD do not call it.
package nodes_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func mustInvoke(t *testing.T, act action.AnyAction, req any) any {
	t.Helper()

	raw, err := action.InvokeAny(context.Background(), act, req)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	return raw
}

// ── bench.run ──────────────────────────────────────────────────────────────

func TestBenchRun_HappyPath(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("noop", action.New("noop",
		func(_ context.Context, _ struct{}) (string, error) { return "ok", nil },
	).Build())

	res := mustInvoke(t, nodes.NewBenchRunAction(reg),
		nodes.BenchRunReq{Action: "noop", Iterations: 100}).(nodes.BenchRunRes)

	if res.Iterations != 100 || res.Errors != 0 {
		t.Fatalf("unexpected: %+v", res)
	}

	if res.P95Ms < res.P50Ms || res.P99Ms < res.P95Ms {
		t.Fatalf("percentile ordering violated: %+v", res)
	}

	if res.RPS <= 0 {
		t.Fatalf("non-positive RPS: %+v", res)
	}
}

func TestBenchRun_NilRegistry(t *testing.T) {
	_, err := action.InvokeAny(context.Background(), nodes.NewBenchRunAction(nil),
		nodes.BenchRunReq{Action: "x", Iterations: 5})
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestBenchRun_TargetNotFound(t *testing.T) {
	reg := flow.NewRegistry()

	_, err := action.InvokeAny(context.Background(), nodes.NewBenchRunAction(reg),
		nodes.BenchRunReq{Action: "missing", Iterations: 5})
	if err == nil {
		t.Fatal("expected NotFound")
	}
}

func TestBenchRun_IterationsExceedLimit(t *testing.T) {
	reg := flow.NewRegistry()

	_, err := action.InvokeAny(context.Background(), nodes.NewBenchRunAction(reg),
		nodes.BenchRunReq{Action: "x", Iterations: 2_000_000})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestBenchRun_PanicIsolation(t *testing.T) {
	reg := flow.NewRegistry()

	var n atomic.Int64

	reg.Register("flaky", action.New("flaky",
		func(_ context.Context, _ struct{}) (string, error) {
			if n.Add(1) == 5 {
				panic("boom")
			}

			return "ok", nil
		},
	).Build())

	res := mustInvoke(t, nodes.NewBenchRunAction(reg),
		nodes.BenchRunReq{Action: "flaky", Iterations: 20, Warmup: 0}).(nodes.BenchRunRes)
	if res.Errors != 1 {
		t.Fatalf("expected exactly 1 error, got %d", res.Errors)
	}
}

func TestBenchRun_ContextCanceledStopsEarly(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("noop", action.New("noop",
		func(_ context.Context, _ struct{}) (string, error) { return "", nil },
	).Build())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := action.InvokeAny(ctx, nodes.NewBenchRunAction(reg),
		nodes.BenchRunReq{Action: "noop", Iterations: 1000})
	if err == nil {
		t.Fatal("expected context.Canceled")
	}
}

// ── bench.save ─────────────────────────────────────────────────────────────

func TestBenchSave_CreatesDirectoryAndWrites(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	res := mustInvoke(t, nodes.NewBenchSaveAction(), nodes.BenchSaveReq{
		File:        "nested/out.json",
		BenchRunRes: nodes.BenchRunRes{Action: "x", Iterations: 10, P95Ms: 12.5},
	}).(nodes.BenchSaveRes)

	if res.Bytes == 0 {
		t.Fatalf("expected non-zero bytes: %+v", res)
	}

	if res.File != "nested/out.json" {
		t.Fatalf("expected clean relative path, got %q", res.File)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "nested", "out.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), `"p95_ms": 12.5`) {
		t.Fatalf("missing p95_ms: %s", string(data))
	}
}

func TestBenchSave_RejectsUnsafePaths(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	for _, p := range []string{
		"/tmp/x.json",       // POSIX absolute
		`C:\Windows\x.json`, // Windows drive
		`\\server\share\x`,  // Windows UNC
		"../escape.json",    // traversal
		"foo/../../escape",  // nested traversal
		"x\x00y",            // NUL
		"con",               // reserved device name
		"file.",             // trailing dot
		"",                  // empty
	} {
		t.Run(p, func(t *testing.T) {
			_, err := action.InvokeAny(context.Background(), nodes.NewBenchSaveAction(),
				nodes.BenchSaveReq{File: p})
			if err == nil {
				t.Fatalf("expected error for unsafe path %q", p)
			}
		})
	}
}

// ── bench.compare ──────────────────────────────────────────────────────────

func TestBenchCompare_RegressionBeyondTolerance(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	_ = os.WriteFile("baseline.json", []byte(`{
		"min_ms":10,"mean_ms":20,"p50_ms":20,"p95_ms":100,"p99_ms":120,"max_ms":130,"rps":1000
	}`), 0o600)

	res := mustInvoke(t, nodes.NewBenchCompareAction(), nodes.BenchCompareReq{
		Baseline: "baseline.json",
		BenchRunRes: nodes.BenchRunRes{
			Action: "x",
			MinMs:  11, MeanMs: 25, P50Ms: 25,
			P95Ms: 150, P99Ms: 200, MaxMs: 220,
			RPS: 800,
		},
	}).(nodes.BenchCompareRes)

	if res.Pass {
		t.Fatalf("expected pass=false: %+v", res)
	}

	if len(res.Regressions) != 7 {
		t.Fatalf("expected 7 regressions, got %v", res.Regressions)
	}

	if d := res.Metrics["p95_ms"].DeltaPct; d < 49 || d > 51 {
		t.Fatalf("p95 delta out of range: %v", d)
	}
}

func TestBenchCompare_WithinTolerancePasses(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	_ = os.WriteFile("b.json", []byte(`{"p95_ms":100,"rps":1000}`), 0o600)

	res := mustInvoke(t, nodes.NewBenchCompareAction(), nodes.BenchCompareReq{
		Baseline:    "b.json",
		BenchRunRes: nodes.BenchRunRes{P95Ms: 103, RPS: 990},
	}).(nodes.BenchCompareRes)
	if !res.Pass {
		t.Fatalf("expected pass: %+v", res)
	}
}

func TestBenchCompare_ZeroBaselineSkipped(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	_ = os.WriteFile("b.json", []byte(`{"p95_ms":0}`), 0o600)

	res := mustInvoke(t, nodes.NewBenchCompareAction(), nodes.BenchCompareReq{
		Baseline:    "b.json",
		BenchRunRes: nodes.BenchRunRes{P95Ms: 500},
	}).(nodes.BenchCompareRes)
	if !res.Pass {
		t.Fatalf("zero baseline must not produce a regression: %+v", res)
	}

	if _, ok := res.Metrics["p95_ms"]; ok {
		t.Fatal("zero baseline must not emit a metric entry")
	}
}

func TestBenchCompare_CorruptBaseline(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	_ = os.WriteFile("bad.json", []byte("not json"), 0o600)

	_, err := action.InvokeAny(context.Background(), nodes.NewBenchCompareAction(),
		nodes.BenchCompareReq{Baseline: "bad.json"})
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestBenchCompare_MissingBaseline(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	_, err := action.InvokeAny(context.Background(), nodes.NewBenchCompareAction(),
		nodes.BenchCompareReq{Baseline: "does-not-exist.json"})
	if err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("expected not-readable error, got %v", err)
	}
}

func TestBenchCompare_RejectsUnsafeBaselinePath(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	for _, p := range []string{
		"/etc/passwd",
		"../baseline.json",
		`C:\baseline.json`,
		"foo:bar",
	} {
		t.Run(p, func(t *testing.T) {
			_, err := action.InvokeAny(context.Background(), nodes.NewBenchCompareAction(),
				nodes.BenchCompareReq{Baseline: p})
			if err == nil {
				t.Fatalf("expected error for unsafe baseline %q", p)
			}
		})
	}
}
