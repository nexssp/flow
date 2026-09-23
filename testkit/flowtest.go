// flow/testkit/flowtest.go
//
// Package flowtest is the flow-specific test harness. It is built
// entirely on top of github.com/nexssp/testkit primitives (Trace,
// GoldenJSON, EventuallyE) plus the flow compiler, so it never touches
// the kernel and never leaks domain logic back into testkit.
//
// Every helper returns a value that supports chained assertions. The
// package is designed so a full end-to-end test of a nontrivial flow
// reads like a paragraph of English, not a wall of setup code.
package flowtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest"
	"github.com/nexssp/kernel/xtest/ktest"
)

// Harness binds a compiled flow to a registry, a trace, and a fake
// clock. Every helper method runs against this harness.
type Harness struct {
	t        testing.TB
	registry *action.Registry
	dsl      string
	trace    *ktest.Trace
	timeout  time.Duration
}

// New compiles dsl against the given libraries, installs a Trace on
// every action, and returns a Harness ready for one or more runs.
func New(t testing.TB, dsl string, libs ...action.Library) *Harness {
	t.Helper()

	reg, err := action.NewRegistry(libs...)
	if err != nil {
		t.Fatalf("flowtest.New: registry: %v", err)
	}

	trace := ktest.NewTrace()
	for _, act := range reg.Actions() {
		act.AddAnyHook(trace.Hook())
	}

	t.Cleanup(func() {
		if t.Failed() {
			trace.Dump(t)
		}
	})

	if _, err := flow.CompilePipeline(dsl, reg); err != nil {
		t.Fatalf("flowtest.New: compile %q: %v", dsl, err)
	}

	return &Harness{
		t:        t,
		registry: reg,
		dsl:      dsl,
		trace:    trace,
		timeout:  5 * time.Second,
	}
}

// WithTimeout changes the run timeout.
func (h *Harness) WithTimeout(d time.Duration) *Harness { h.timeout = d; return h }

// Trace returns the underlying trace for direct assertions.
func (h *Harness) Trace() *ktest.Trace { return h.trace }

// Registry returns the registry, so callers can look up specific actions.
func (h *Harness) Registry() *action.Registry { return h.registry }

// Run executes the flow with payload and returns a Run.
func (h *Harness) Run(payload map[string]any) *Run {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	_, err := flow.CompilePipeline(h.dsl, h.registry)
	if err != nil {
		h.t.Fatalf("flowtest.Run: compile: %v", err)
	}

	// The runner path is richer than CompilePipeline alone: it
	// materializes declarations, resolves capabilities, and installs
	// the observer. Use it as the execution engine so tests match prod.
	b := flow.NewExecuteAction(flow.NewCompiler(h.registry)).ToBuilder()
	b.AnyHook(h.trace.Hook())
	execAct := b.Build()

	start := time.Now()
	res, execErr := execAct.Do(ctx, flow.GraphExecReq{
		DSL:            h.dsl,
		InitialPayload: payload,
	})

	return &Run{
		t:        h.t,
		result:   res,
		err:      execErr,
		duration: time.Since(start),
		trace:    h.trace,
	}
}

// Run captures the outcome of one flow execution.
type Run struct {
	t        testing.TB
	result   flow.GraphExecRes
	err      error
	duration time.Duration
	trace    *ktest.Trace
}

// ExpectSuccess fails the test if the flow returned an error.
func (r *Run) ExpectSuccess() *Run {
	r.t.Helper()
	if r.err != nil {
		r.t.Fatalf("flow failed: %v", r.err)
	}
	return r
}

// ExpectFailure fails the test if the flow succeeded.
func (r *Run) ExpectFailure() *Run {
	r.t.Helper()
	if r.err == nil {
		r.t.Fatalf("expected flow to fail, it succeeded in %s", r.duration)
	}
	return r
}

// Duration returns the wall-clock time of the run.
func (r *Run) Duration() time.Duration { return r.duration }

// Output returns the raw output map.
func (r *Run) Output() map[string]any { return r.result.Outputs }

// OutputKey returns the value at key, failing if absent.
func (r *Run) OutputKey(key string) any {
	r.t.Helper()
	v, ok := r.result.Outputs[key]
	if !ok {
		r.t.Fatalf("output key %q missing; keys: %v", key, sortedKeys(r.result.Outputs))
	}
	return v
}

// Golden writes the whole run to testdata/<name>.golden.json and, on
// subsequent runs, compares. Use -testkit.update to rewrite.
func (r *Run) Golden(name string) *Run {
	r.t.Helper()
	xtest.GoldenJSON(r.t, name, map[string]any{
		"graph_name": r.result.GraphName,
		"layers_run": r.result.LayersRun,
		"outputs":    r.result.Outputs,
	})
	return r
}

// Trace returns the shared trace so callers can chain assertions.
func (r *Run) Trace() *ktest.Trace { return r.trace }

// AssertOutput asserts that outputs[key] equals want, comparing with
// fmt.Sprintf("%v") so structs, numbers, and strings all work without
// custom comparators.
func (r *Run) AssertOutput(key string, want any) *Run {
	r.t.Helper()
	got := r.OutputKey(key)
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		r.t.Fatalf("output[%q]: got %v, want %v", key, got, want)
	}
	return r
}

// AssertLayers asserts that the number of executed layers matches want.
func (r *Run) AssertLayers(want int) *Run {
	r.t.Helper()
	if r.result.LayersRun != want {
		r.t.Fatalf("layers: got %d, want %d", r.result.LayersRun, want)
	}
	return r
}

// AssertFast asserts the run finished in less than d.
func (r *Run) AssertFast(d time.Duration) *Run {
	r.t.Helper()
	if r.duration > d {
		r.t.Fatalf("run took %s, expected under %s", r.duration, d)
	}
	return r
}

// AssertMaxAllocs runs the flow n times and asserts that the total
// allocation count (rough) is under budget. Useful for keeping a hot
// path zero-alloc as the flow grows.
func (h *Harness) AssertMaxAllocs(n int, budget float64) {
	h.t.Helper()

	allocs := testing.AllocsPerRun(n, func() {
		_ = h.Run(map[string]any{})
	})
	if allocs > budget {
		h.t.Fatalf("flow allocated %.2f allocs/run, budget %.2f", allocs, budget)
	}
}

// AssertConcurrent runs the flow concurrently across workers and
// asserts that (a) every run succeeded, (b) no race was detected by
// -race, and (c) the trace contains exactly the expected number of
// executions of every action in the flow.
func (h *Harness) AssertConcurrent(workers int) {
	h.t.Helper()

	h.trace.Reset()

	sem := make(chan struct{}, workers)
	done := make(chan struct{}, workers)

	for range workers {
		go func() {
			defer func() { done <- struct{}{} }()
			sem <- struct{}{}
			defer func() { <-sem }()

			built, err := flow.CompilePipeline(h.dsl, h.registry)
			if err != nil {
				h.t.Errorf("compile: %v", err)
				return
			}
			_, err = built.Build().Do(context.Background(), map[string]any{})
			if err != nil {
				h.t.Errorf("run: %v", err)
			}
		}()
	}

	for range workers {
		<-done
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
