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
}

// Result separates one compilation and first execution from warm samples.
type Result struct {
	Name       string                  `json:"name"`
	CompileMs  float64                 `json:"compile_ms"`
	FirstRunMs float64                 `json:"first_run_ms"`
	Warmup     int                     `json:"warmup"`
	Warm       nodes_bench.BenchRunRes `json:"warm"`
	Error      string                  `json:"error,omitempty"`
}

type Report struct {
	Version    int      `json:"version"`
	GoVersion  string   `json:"go_version"`
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	CPUs       int      `json:"cpus"`
	GOMAXPROCS int      `json:"gomaxprocs"`
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
	report := Report{Version: 1, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), Results: make([]Result, 0, len(scenarios))}
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
	start := time.Now()
	compiled, err := runner.Compile(ctx, cfg, scenario.Source, scenario.File)
	result.CompileMs = milliseconds(time.Since(start))
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
	start = time.Now()
	err = invoke(ctx)
	result.FirstRunMs = milliseconds(time.Since(start))
	if err != nil {
		result.Error = "first run: " + err.Error()
		return result
	}
	for range opts.Warmup {
		if err = invoke(ctx); err != nil {
			result.Error = "warmup: " + err.Error()
			return result
		}
	}
	result.Warm, err = nodes_bench.Measure(ctx, opts.Iterations, invoke)
	result.Warm.Action = scenario.Name
	result.Warm.Warmup = opts.Warmup
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
