package selfbench

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/nodes_bench"
)

func TestWriteReportHumanIncludesMetricsAndFailures(t *testing.T) {
	report := Report{
		Profiled:   true,
		GoVersion:  "go1.23.4",
		OS:         "linux",
		Arch:       "amd64",
		CPUs:       8,
		GOMAXPROCS: 4,
		Results: []Result{
			{
				Name: "ok.nflow", CompileMs: 1.25, CompileAllocation: AllocationStats{Allocations: 12, Bytes: 2048},
				FirstRunMs: 2.5, FirstRunAllocation: AllocationStats{Allocations: 5, Bytes: 512}, Warmup: 3,
				WarmupAllocation: AllocationPerRun{Runs: 3, AllocationsPerRun: 4, BytesPerRun: 256},
				WarmAllocation:   AllocationPerRun{Runs: 5, AllocationsPerRun: 6, BytesPerRun: 128},
				Warm:             nodes_bench.BenchRunRes{Iterations: 5, MinMs: 0.1, MeanMs: 0.2, MaxMs: 0.5, P50Ms: 0.15, P95Ms: 0.4, P99Ms: 0.5, RPS: 123.4},
				Actions:          []ActionProfile{{Action: "pipeline.normalize", Calls: 5, TotalMs: 1.2, MeanMs: 0.24, MaxMs: 0.3}},
			},
			{
				Name: "bad.nflow", CompileMs: 3, FirstRunMs: 4, Warmup: 2, Error: "first run: expected failure",
				Warm: nodes_bench.BenchRunRes{Iterations: 7, MinMs: 0.2, MeanMs: 0.3, MaxMs: 0.6, P50Ms: 0.25, P95Ms: 0.5, P99Ms: 0.6, RPS: 45.6, Errors: 1},
			},
		},
	}
	var output bytes.Buffer
	ktest.RequireNoError(t, writeReport(report, Options{Out: &output}))
	text := output.String()
	for _, want := range []string{
		"NEXSS FLOW · SELF BENCH", "in-process timing; allocations are Go heap bytes created, not live memory or RSS",
		"HOST go1.23.4 · linux/amd64 · 8 CPUs · GOMAXPROCS=4",
		"action profile enabled; profiling hooks are included in measured timings",
		"▸ ok.nflow", "✓ passed", "Compilation", "1.250 ms", "First run", "2.500 ms", "Warmup", "3 runs",
		"Warm samples", "5 samples", "min 0.100 · mean 0.200 · max 0.500 ms",
		"p50 0.150 · p95 0.400 · p99 0.500 ms", "123.4 requests/s", "Sample errors",
		"PHASE GRAPH", "compile", "first run", "warmup / run", "warm / run", "2.0 KiB", "512 B",
		"ACTION GRAPH", "pipeline.normalize", "0.240 ms/call", "5 calls", "nested actions can overlap",
		"▸ bad.nflow", "✗ failed", "7 samples", "45.6 requests/s", "1 passed", "1 failed", "(2 total)", "first run: expected failure",
	} {
		ktest.RequireStringContains(t, text, want)
	}
	ktest.RequireEqual(t, false, strings.Contains(text, "\x1b["))
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteReportPropagatesHumanWriterError(t *testing.T) {
	wantErr := errors.New("broken output")
	err := writeReport(Report{}, Options{Out: failingWriter{err: wantErr}})
	ktest.RequireCondition(t, errors.Is(err, wantErr), "must propagate writer failure: %v", err)
}

func TestWriteReportJSONIncludesAllocationAndProfileMetrics(t *testing.T) {
	report := Report{
		Version: 1, GoVersion: "go1", OS: "linux", Arch: "amd64", CPUs: 2, GOMAXPROCS: 2, Profiled: true,
		Results: []Result{{
			Name: "ok.nflow", CompileMs: 1, CompileAllocation: AllocationStats{Allocations: 2, Bytes: 64},
			FirstRunMs: 2, FirstRunAllocation: AllocationStats{Allocations: 3, Bytes: 128}, Warmup: 3,
			WarmupAllocation: AllocationPerRun{Runs: 3, AllocationsPerRun: 4, BytesPerRun: 256},
			WarmAllocation:   AllocationPerRun{Runs: 4, AllocationsPerRun: 5.5, BytesPerRun: 300},
			Warm:             nodes_bench.BenchRunRes{Action: "ok.nflow", Iterations: 4, Warmup: 3, MinMs: 0.1, MaxMs: 0.5, MeanMs: 0.3, P50Ms: 0.2, P95Ms: 0.5, P99Ms: 0.5, RPS: 10, ElapsedMs: 400},
			Actions:          []ActionProfile{{Action: "runtime.echo", Calls: 4, TotalMs: 0.8, MeanMs: 0.2, MaxMs: 0.3}},
		}},
	}
	var output bytes.Buffer
	ktest.RequireNoError(t, writeReport(report, Options{Out: &output, JSON: true}))
	const want = `{
  "version": 1,
  "go_version": "go1",
  "os": "linux",
  "arch": "amd64",
  "cpus": 2,
  "gomaxprocs": 2,
  "profiled": true,
  "results": [
    {
      "name": "ok.nflow",
      "compile_ms": 1,
      "compile_allocation": {
        "allocations": 2,
        "bytes": 64
      },
      "first_run_ms": 2,
      "first_run_allocation": {
        "allocations": 3,
        "bytes": 128
      },
      "warmup": 3,
      "warmup_allocation": {
        "runs": 3,
        "allocations_per_run": 4,
        "bytes_per_run": 256
      },
      "warm": {
        "action": "ok.nflow",
        "iterations": 4,
        "warmup": 3,
        "errors": 0,
        "min_ms": 0.1,
        "max_ms": 0.5,
        "mean_ms": 0.3,
        "p50_ms": 0.2,
        "p95_ms": 0.5,
        "p99_ms": 0.5,
        "rps": 10,
        "elapsed_ms": 400
      },
      "warm_allocation": {
        "runs": 4,
        "allocations_per_run": 5.5,
        "bytes_per_run": 300
      },
      "actions": [
        {
          "action": "runtime.echo",
          "calls": 4,
          "total_ms": 0.8,
          "mean_ms": 0.2,
          "max_ms": 0.3
        }
      ]
    }
  ]
}
`
	ktest.RequireEqual(t, want, output.String())
}
