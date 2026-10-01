package selftest

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/core"
	flowrunner "github.com/nexssp/flow/runner"
)

type Status uint8

const (
	StatusPass Status = iota
	StatusFail
	StatusSkip
)

// Result is one feature's outcome.
//
// Elapsed covers the whole runner.Execute call. CompileAllocs and
// RunAllocs split the allocation count by phase: the compile phase is
// the compiler's cost for parsing and building this DSL fragment, the
// run phase is the actual runtime cost of the pipeline. In a feature
// like `noop` all the work is in CompileAllocs and RunAllocs is zero.
type Result struct {
	core.SelfTestFeature
	Section           string
	Status            Status
	Elapsed           time.Duration
	CompileAllocs     uint64
	CompileAllocBytes uint64
	RunAllocs         uint64
	RunAllocBytes     uint64
	Err               error
	AssertErrs        []error
	SkipReason        string
	Captured          []byte
}

// TotalAllocs is the per-feature sum of compile and run allocations.
func (r Result) TotalAllocs() uint64 { return r.CompileAllocs + r.RunAllocs }

// TotalAllocBytes is the per-feature sum of compile and run bytes.
func (r Result) TotalAllocBytes() uint64 { return r.CompileAllocBytes + r.RunAllocBytes }

var outputMutex sync.Mutex

// captureResult carries stdout/stderr and the output of one feature.
type captureResult struct {
	captured []byte
	output   any
	err      error
}

// captureExecution redirects stdout and stderr into a pipe for the
// duration of runFunc. Allocation measurement happens inside
// runner.Execute, not here: the phase boundaries live there.
func captureExecution(runFunc func() (any, error)) captureResult {
	outputMutex.Lock()
	defer outputMutex.Unlock()

	previousStdout := os.Stdout
	previousStderr := os.Stderr
	previousLogger := slog.Default()

	pipeReader, pipeWriter, pipeErr := os.Pipe()
	if pipeErr != nil {
		output, err := runFunc()
		return captureResult{output: output, err: err}
	}

	os.Stdout = pipeWriter
	os.Stderr = pipeWriter
	slog.SetDefault(slog.New(slog.NewTextHandler(pipeWriter, nil)))

	var buffer bytes.Buffer
	doneChannel := make(chan struct{})
	go func() {
		defer close(doneChannel)
		if _, err := buffer.ReadFrom(pipeReader); err != nil {
			slog.Debug("selftest: pipe read ended early", "err", err)
		}
	}()

	output, err := runFunc()

	_ = pipeWriter.Close()
	os.Stdout = previousStdout
	os.Stderr = previousStderr
	slog.SetDefault(previousLogger)
	<-doneChannel

	return captureResult{
		captured: buffer.Bytes(),
		output:   output,
		err:      err,
	}
}

// runFeature executes one feature through the production runner with
// allocation measurement enabled. The config is built once per Run and
// passed in; the phase boundaries for allocation measurement live
// inside runner.Execute, not here.
func runFeature(ctx context.Context, cfg flowrunner.Config, feature core.SelfTestFeature) Result {
	started := time.Now()

	cleanup, err := prepareFeatureDir(feature)
	if err != nil {
		return Result{SelfTestFeature: feature, Status: StatusFail, Err: err, Elapsed: time.Since(started)}
	}
	defer cleanup()

	execCtx := xctx.WithUserID(ctx, "selftest")
	execCtx = xctx.WithTenantID(execCtx, "selftest-tenant")
	execCtx = xctx.WithRoles(execCtx, []string{"admin", "system_admin"})
	execCtx = xctx.WithPermissions(execCtx, []string{"*"})
	execCtx = xctx.WithApprovalToken(execCtx, "all")

	capture := captureExecution(func() (any, error) {
		ex, err := flowrunner.Execute(execCtx, cfg, feature.DSL, feature.Name, map[string]any{})
		if err != nil {
			return nil, err
		}
		return ex, nil
	})

	elapsed := time.Since(started)

	if capture.err != nil {
		return Result{
			SelfTestFeature: feature,
			Status:          StatusFail,
			Err:             capture.err,
			Elapsed:         elapsed,
			Captured:        capture.captured,
		}
	}

	ex, ok := capture.output.(flowrunner.Execution)
	if !ok {
		return Result{
			SelfTestFeature: feature,
			Status:          StatusFail,
			Err:             fmt.Errorf("selftest: Execute returned %T, want Execution", capture.output),
			Elapsed:         elapsed,
		}
	}

	asserts, _ := ex.Meta["asserts"].([]string)
	assertErrors := evalAsserts(ex.Output, asserts)
	if len(assertErrors) > 0 {
		return Result{
			SelfTestFeature:   feature,
			Status:            StatusFail,
			Err:               assertErrors[0],
			AssertErrs:        assertErrors,
			Elapsed:           elapsed,
			CompileAllocs:     ex.CompileAllocs,
			CompileAllocBytes: ex.CompileAllocBytes,
			RunAllocs:         ex.RunAllocs,
			RunAllocBytes:     ex.RunAllocBytes,
			Captured:          capture.captured,
		}
	}

	return Result{
		SelfTestFeature:   feature,
		Status:            StatusPass,
		Elapsed:           elapsed,
		CompileAllocs:     ex.CompileAllocs,
		CompileAllocBytes: ex.CompileAllocBytes,
		RunAllocs:         ex.RunAllocs,
		RunAllocBytes:     ex.RunAllocBytes,
	}
}

// prepareFeatureDir materializes feature.Files into a temp directory
// and chdirs there so relative paths in the DSL resolve as written.
// Returns a no-op cleanup for features without Files.
func prepareFeatureDir(feature core.SelfTestFeature) (cleanup func(), err error) {
	if len(feature.Files) == 0 {
		return func() {}, nil
	}

	tempDir, err := os.MkdirTemp("", "nflow-selftest-")
	if err != nil {
		return nil, err
	}

	removeAll := func() { _ = os.RemoveAll(tempDir) }

	for name, content := range feature.Files {
		target := filepath.Join(tempDir, name)
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			removeAll()
			return nil, err
		}
		writeErr := os.WriteFile(target, []byte(content), 0o600)
		if writeErr != nil {
			removeAll()
			return nil, writeErr
		}
	}

	prevDir, err := os.Getwd()
	if err != nil {
		removeAll()
		return nil, err
	}
	if err := os.Chdir(tempDir); err != nil {
		removeAll()
		return nil, err
	}

	return func() {
		if chErr := os.Chdir(prevDir); chErr != nil {
			slog.Warn("selftest: chdir back failed", "err", chErr)
		}
		removeAll()
	}, nil
}

func evalAsserts(result any, assertions []string) []error {
	if len(assertions) == 0 {
		return nil
	}

	normalized := flowrunner.JSONMapView(result)
	environment := map[string]any{
		"result": normalized,
	}
	if resultMap, ok := normalized.(map[string]any); ok {
		maps.Copy(environment, resultMap)
	}

	var errorsList []error
	for _, assertionExpr := range assertions {
		program, err := expr.Compile(assertionExpr, expr.Env(environment))
		if err != nil {
			errorsList = append(errorsList, fmt.Errorf("assert %q syntax: %w", assertionExpr, err))
			continue
		}

		output, err := expr.Run(program, environment)
		if err != nil {
			errorsList = append(errorsList, fmt.Errorf("assert %q runtime: %w", assertionExpr, err))
			continue
		}

		if passed, ok := output.(bool); !ok || !passed {
			errorsList = append(errorsList, fmt.Errorf("assert %q failed (got %v)", assertionExpr, output))
		}
	}

	return errorsList
}
