package flow_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type ValidationTarget struct {
	Name  string `json:"name" validate:"required,min=5"`
	Email string `json:"email" validate:"required,email"`
}

func TestDSL_Modifiers_Timeout(t *testing.T) {
	t.Parallel()

	slowAct := action.New("slow.op", func(ctx context.Context, _ any) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
			return "done", nil
		}
	}).Build()

	reg := flow.NewRegistry(slowAct)

	// DSL attaches :timeout=50ms dynamically via Kernel
	pipeline, err := flow.CompilePipeline("slow.op:timeout=50ms", reg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	_, err = pipeline.Build().Do(context.Background(), nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded error from :timeout=50ms modifier, got: %v", err)
	}
}

func TestDSL_Modifiers_Retry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	flakyAct := action.New("flaky.op", func(_ context.Context, _ any) (string, error) {
		count := attempts.Add(1)
		if count < 3 {
			// Transient error recognized by xerr.IsTransient triggers Kernel retry
			return "", xerr.Unavailable("temporary upstream network drop")
		}

		return "recovered", nil
	}).Build()

	reg := flow.NewRegistry(flakyAct)

	// DSL attaches :retry=3 dynamically
	pipeline, err := flow.CompilePipeline("flaky.op:retry=3", reg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	res, err := pipeline.Build().Do(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected recovery after retries, got: %v", err)
	}

	if res != "recovered" || attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts and 'recovered' result, got attempts=%d res=%v", attempts.Load(), res)
	}
}

func TestDSL_Modifiers_Validation(t *testing.T) {
	t.Parallel()

	saveAct := action.New("user.save", func(_ context.Context, u ValidationTarget) (string, error) {
		return "saved:" + u.Name, nil
	}).Build()

	reg := flow.NewRegistry(saveAct)

	// DSL attaches :validate
	pipeline, err := flow.CompilePipeline("user.save:validate", reg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	built := pipeline.Build()

	// 1. Valid payload passes
	valid := ValidationTarget{Name: "Alexander", Email: "alex@nexss.com"}

	res, err := built.Do(context.Background(), valid)
	if err != nil || res != "saved:Alexander" {
		t.Fatalf("valid payload failed validation: %v", err)
	}

	// 2. Invalid payload (name too short, invalid email) rejected immediately
	invalid := ValidationTarget{Name: "Al", Email: "not-an-email"}

	_, err = built.Do(context.Background(), invalid)
	if err == nil {
		t.Fatal("expected validation error on invalid struct, got nil")
	}
}

func TestDSL_Modifiers_Cache(t *testing.T) {
	t.Parallel()

	var dbCalls atomic.Int32

	dbAct := action.New("db.query", func(_ context.Context, id int) (string, error) {
		dbCalls.Add(1)

		return "data_payload", nil
	}).Build()

	reg := flow.NewRegistry(dbAct)

	// DSL attaches :cache=1h
	pipeline, err := flow.CompilePipeline("db.query:cache=1h", reg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	built := pipeline.Build()

	// First call -> hits DB
	_, _ = built.Do(context.Background(), 42)
	// Second call with same key -> served from cache
	_, _ = built.Do(context.Background(), 42)
	// Third call with different key -> hits DB
	_, _ = built.Do(context.Background(), 99)

	if dbCalls.Load() != 2 {
		t.Fatalf("expected 2 DB calls (1 cached hit), got %d", dbCalls.Load())
	}
}

func TestDSL_Modifiers_Coalesce(t *testing.T) {
	t.Parallel()

	var heavyCalls atomic.Int32

	gate := make(chan struct{})

	heavyAct := action.New("heavy.compute", func(_ context.Context, id string) (string, error) {
		heavyCalls.Add(1)
		<-gate // Hold until all callers join

		return "computed:" + id, nil
	}).Build()

	reg := flow.NewRegistry(heavyAct)

	// DSL attaches :coalesce
	pipeline, err := flow.CompilePipeline("heavy.compute:coalesce", reg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	built := pipeline.Build()

	const concurrent = 10

	var wg sync.WaitGroup
	wg.Add(concurrent)

	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()

			_, _ = built.Do(context.Background(), "same_key")
		}()
	}

	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()

	if heavyCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 execution across %d concurrent callers, got %d", concurrent, heavyCalls.Load())
	}
}

func TestDSL_AttributesAndParameters_Injection(t *testing.T) {
	t.Parallel()

	var capturedMap map[string]any

	workerAct := action.New("tool.run", func(_ context.Context, req map[string]any) (string, error) {
		capturedMap = req

		return "ok", nil
	}).Build()

	reg := flow.NewRegistry(workerAct)

	// Test complex AST token decorations:
	// - Profile: :arch
	// - Target: #internal/auth,pkg/api
	// - Exclude: ~testdata,mocks
	// - Prompt: @security_audit
	// - Parameters: (env="staging", max_depth=5)
	dsl := `tool.run:arch#internal/auth,pkg/api~testdata,mocks@security_audit(env="staging", max_depth=5)`

	pipeline, err := flow.CompilePipeline(dsl, reg)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	input := map[string]any{"user_id": "usr_100"}

	_, err = pipeline.Build().Do(context.Background(), input)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if capturedMap["profile"] != "arch" {
		t.Errorf("expected profile 'arch', got %v", capturedMap["profile"])
	}

	if capturedMap["prompt"] != "security_audit" {
		t.Errorf("expected prompt 'security_audit', got %v", capturedMap["prompt"])
	}

	if capturedMap["env"] != "staging" {
		t.Errorf("expected env 'staging', got %v", capturedMap["env"])
	}

	if capturedMap["max_depth"] != "5" {
		t.Errorf("expected max_depth '5', got %v", capturedMap["max_depth"])
	}

	targets, ok := capturedMap["targets"].([]string)
	if !ok || len(targets) != 2 || targets[0] != "internal/auth" || targets[1] != "pkg/api" {
		t.Errorf("unexpected targets: %v", capturedMap["targets"])
	}

	excludes, ok := capturedMap["excludes"].([]string)
	if !ok || len(excludes) != 2 || excludes[0] != "testdata" || excludes[1] != "mocks" {
		t.Errorf("unexpected excludes: %v", capturedMap["excludes"])
	}
}
