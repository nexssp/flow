package selfbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/extensions/nodes_bench"
	"github.com/nexssp/flow/runner"
)

// Options controls measurement; zero warmup is meaningful.
type Options struct {
	Out        io.Writer
	Iterations int
	Warmup     int
	JSON       bool
	List       bool
	Profile    bool
}

// AllocationStats counts Go heap allocations made during a phase. Bytes are
// cumulative allocated bytes, not live heap size or resident memory.
type AllocationStats struct {
	Allocations uint64 `json:"allocations"`
	Bytes       uint64 `json:"bytes"`
}

// AllocationPerRun summarizes allocations over repeated runs.
type AllocationPerRun struct {
	Runs              int     `json:"runs"`
	AllocationsPerRun float64 `json:"allocations_per_run"`
	BytesPerRun       float64 `json:"bytes_per_run"`
}

// Result separates compilation, first execution, and warm samples.
type Result struct {
	Name               string                  `json:"name"`
	CompileMs          float64                 `json:"compile_ms"`
	CompileAllocation  AllocationStats         `json:"compile_allocation"`
	FirstRunMs         float64                 `json:"first_run_ms"`
	FirstRunAllocation AllocationStats         `json:"first_run_allocation"`
	Warmup             int                     `json:"warmup"`
	WarmupAllocation   AllocationPerRun        `json:"warmup_allocation"`
	Warm               nodes_bench.BenchRunRes `json:"warm"`
	WarmAllocation     AllocationPerRun        `json:"warm_allocation"`
	Actions            []ActionProfile         `json:"actions,omitempty"`
	Error              string                  `json:"error,omitempty"`
}

type Report struct {
	Version    int      `json:"version"`
	GoVersion  string   `json:"go_version"`
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	CPUs       int      `json:"cpus"`
	GOMAXPROCS int      `json:"gomaxprocs"`
	Profiled   bool     `json:"profiled"`
	Results    []Result `json:"results"`
}

// Run returns 0 on success, 1 on compilation/execution/output failure, and 2
// on invalid options. Config must belong to the caller's live runner Host.
func Run(ctx context.Context, cfg runner.Config, scenarios []Scenario, opts Options) int {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Iterations < 1 || opts.Iterations > 1_000_000 || opts.Warmup < 0 || opts.Warmup > 1_000_000 || len(scenarios) == 0 {
		fmt.Fprintln(opts.Out, "self bench: iterations must be 1..1000000, warmup 0..1000000, and scenarios nonempty")
		return 2
	}
	if opts.List {
		if err := listScenarios(scenarios, opts); err != nil {
			return 1
		}
		return 0
	}
	report := Report{Version: 1, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), Profiled: opts.Profile, Results: make([]Result, 0, len(scenarios))}
	code := 0
	for _, scenario := range scenarios {
		result := measure(ctx, cfg, scenario, opts)
		report.Results = append(report.Results, result)
		if result.Error != "" || result.Warm.Errors > 0 {
			code = 1
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := writeReport(report, opts); err != nil {
		return 1
	}
	return code
}

func listScenarios(scenarios []Scenario, opts Options) error {
	names := make([]string, len(scenarios))
	for i, scenario := range scenarios {
		names[i] = scenario.Name
	}
	if opts.JSON {
		return json.NewEncoder(opts.Out).Encode(struct {
			Scenarios []string `json:"scenarios"`
		}{names})
	}
	for _, name := range names {
		if _, err := fmt.Fprintln(opts.Out, name); err != nil {
			return err
		}
	}
	return nil
}

func writeReport(report Report, opts Options) error {
	if opts.JSON {
		encoder := json.NewEncoder(opts.Out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return renderReport(report, opts.Out)
}

func measure(ctx context.Context, cfg runner.Config, scenario Scenario, opts Options) (result Result) {
	result.Name = scenario.Name
	result.Warmup = opts.Warmup
	var profile *actionProfiler
	if opts.Profile {
		profile = newActionProfiler()
		cfg.EventSink = combineEventSinks(cfg.EventSink, profile)
	}
	// A faulty custom scenario must be a failed benchmark, not crash the suite.
	defer func() {
		if p := recover(); p != nil {
			result.Error = xerr.PanicRecovery(p).Error()
		}
	}()
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return result
	}
	compileBefore := readAllocationCounter()
	start := time.Now()
	compiled, err := runner.Compile(ctx, cfg, scenario.Source, scenario.File)
	result.CompileMs = milliseconds(time.Since(start))
	result.CompileAllocation = allocationDelta(compileBefore, readAllocationCounter())
	if err != nil {
		result.Error = "compile: " + err.Error()
		return result
	}
	invoke := func(ctx context.Context) error {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		execution, runErr := runner.ExecuteCompiled(ctx, cfg, compiled, scenario.Source, scenario.File, map[string]any{})
		if runErr != nil {
			return runErr
		}
		if execution.Suspension != nil {
			return xerr.BadRequest("self bench: scenario suspended for approval")
		}
		return nil
	}
	firstBefore := readAllocationCounter()
	start = time.Now()
	err = invoke(ctx)
	result.FirstRunMs = milliseconds(time.Since(start))
	result.FirstRunAllocation = allocationDelta(firstBefore, readAllocationCounter())
	if err != nil {
		result.Error = "first run: " + err.Error()
		return result
	}
	if opts.Warmup > 0 {
		warmupBefore := readAllocationCounter()
		warmupRuns := 0
		for range opts.Warmup {
			warmupRuns++
			if err = invoke(ctx); err != nil {
				result.WarmupAllocation = allocationAverage(allocationDelta(warmupBefore, readAllocationCounter()), warmupRuns)
				result.Error = "warmup: " + err.Error()
				return result
			}
		}
		result.WarmupAllocation = allocationAverage(allocationDelta(warmupBefore, readAllocationCounter()), warmupRuns)
	}
	warmBefore := readAllocationCounter()
	result.Warm, err = measureWarm(ctx, opts.Iterations, invoke, profile)
	result.WarmAllocation = allocationAverage(allocationDelta(warmBefore, readAllocationCounter()), result.Warm.Iterations)
	if profile != nil {
		result.Actions = profile.Snapshot()
	}
	result.Warm.Action = scenario.Name
	result.Warm.Warmup = opts.Warmup
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func measureWarm(ctx context.Context, iterations int, invoke func(context.Context) error, profile *actionProfiler) (nodes_bench.BenchRunRes, error) {
	if profile == nil {
		return nodes_bench.Measure(ctx, iterations, invoke)
	}
	profile.SetEnabled(true)
	defer profile.SetEnabled(false)
	return nodes_bench.Measure(ctx, iterations, invoke)
}

type allocationCounter struct {
	objects uint64
	bytes   uint64
}

func readAllocationCounter() allocationCounter {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return allocationCounter{objects: stats.Mallocs, bytes: stats.TotalAlloc}
}

func allocationDelta(before, after allocationCounter) AllocationStats {
	return AllocationStats{Allocations: after.objects - before.objects, Bytes: after.bytes - before.bytes}
}

func allocationAverage(stats AllocationStats, runs int) AllocationPerRun {
	if runs < 1 {
		return AllocationPerRun{}
	}
	return AllocationPerRun{
		Runs:              runs,
		AllocationsPerRun: float64(stats.Allocations) / float64(runs),
		BytesPerRun:       float64(stats.Bytes) / float64(runs),
	}
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
