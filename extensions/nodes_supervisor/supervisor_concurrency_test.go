package nodes_supervisor

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler/contracts"
)

type concurrencyBoundCompiler struct {
	started chan struct{}
	release <-chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
}

func (c *concurrencyBoundCompiler) CompilePipeline(string) (action.Executable, error) {
	return concurrencyBoundExecutable{compiler: c}, nil
}

type concurrencyBoundExecutable struct {
	compiler *concurrencyBoundCompiler
}

func (e concurrencyBoundExecutable) ExecuteDecoded(ctx context.Context, _ action.DecodeFunc) (any, error) {
	active := e.compiler.active.Add(1)
	for {
		peak := e.compiler.peak.Load()
		if active <= peak || e.compiler.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	e.compiler.started <- struct{}{}
	select {
	case <-e.compiler.release:
	case <-ctx.Done():
	}
	e.compiler.active.Add(-1)
	return nil, nil
}

func TestSupervisorBoundsConcurrentChildPipelines(t *testing.T) {
	tasks := make([]ChildTask, maxChildConcurrency+1)
	started := make(chan struct{}, len(tasks))
	release := make(chan struct{})
	compiler := &concurrencyBoundCompiler{started: started, release: release}
	ctx, cancel := context.WithCancel(contracts.WithCompiler(t.Context(), compiler))
	released := false
	defer func() {
		cancel()
		if !released {
			close(release)
		}
	}()

	type result struct {
		value SupervisorRes
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := Supervisor.Do(ctx, SupervisorReq{Tasks: tasks})
		done <- result{value: value, err: err}
	}()

	for i := range maxChildConcurrency {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d children started; want %d", i, maxChildConcurrency)
		}
	}
	if got := compiler.active.Load(); got != int32(maxChildConcurrency) {
		t.Fatalf("active child count = %d, want %d", got, maxChildConcurrency)
	}

	close(release)
	released = true
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Supervisor.Do: %v", result.err)
		}
		if result.value.Total != len(tasks) || result.value.Succeeded != len(tasks) {
			t.Fatalf("supervisor result = total %d, succeeded %d; want %d", result.value.Total, result.value.Succeeded, len(tasks))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not finish after releasing child pipelines")
	}
	if got := compiler.peak.Load(); got != int32(maxChildConcurrency) {
		t.Fatalf("peak child concurrency = %d, want %d", got, maxChildConcurrency)
	}
}

type cancellationCompiler struct {
	started chan struct{}
	release chan struct{}
	count   atomic.Int32
}

func (c *cancellationCompiler) CompilePipeline(string) (action.Executable, error) {
	c.count.Add(1)
	c.started <- struct{}{}
	<-c.release
	return cancellationExecutable{}, nil
}

type cancellationExecutable struct{}

func (cancellationExecutable) ExecuteDecoded(context.Context, action.DecodeFunc) (any, error) {
	return nil, nil
}

func TestSupervisorStopsSubmittingAfterCancellation(t *testing.T) {
	tasks := make([]ChildTask, maxChildConcurrency+1)
	for i := range tasks {
		tasks[i].ID = "child-" + strconv.Itoa(i)
	}
	compiler := &cancellationCompiler{started: make(chan struct{}, len(tasks)), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(contracts.WithCompiler(t.Context(), compiler))
	released := false
	defer func() {
		cancel()
		if !released {
			close(compiler.release)
		}
	}()
	type result struct {
		value SupervisorRes
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := Supervisor.Do(ctx, SupervisorReq{Tasks: tasks})
		done <- result{value: value, err: err}
	}()

	for i := range maxChildConcurrency {
		select {
		case <-compiler.started:
		case <-time.After(3 * time.Second):
			cancel()
			close(compiler.release)
			released = true
			t.Fatalf("only %d child compilations started; want %d", i, maxChildConcurrency)
		}
	}
	cancel()
	close(compiler.release)
	released = true
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Supervisor.Do: %v", result.err)
		}
		if got := compiler.count.Load(); got != int32(maxChildConcurrency) {
			t.Fatalf("CompilePipeline calls = %d, want %d", got, maxChildConcurrency)
		}
		if result.value.Results[maxChildConcurrency].Error != context.Canceled.Error() {
			t.Fatalf("unscheduled task error = %q, want %q", result.value.Results[maxChildConcurrency].Error, context.Canceled)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not finish after cancellation")
	}
}

func TestRunChildSkipsCompileWhenContextCanceled(t *testing.T) {
	compiler := &cancellationCompiler{started: make(chan struct{}, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runChild(ctx, compiler, ChildTask{ID: "canceled"})
	if result.Error != context.Canceled.Error() {
		t.Fatalf("child error = %q, want %q", result.Error, context.Canceled)
	}
	if got := compiler.count.Load(); got != 0 {
		t.Fatalf("CompilePipeline calls = %d, want 0", got)
	}
}
