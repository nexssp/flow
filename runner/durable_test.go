package runner

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/loop"
	"github.com/nexssp/flow/extensions/modifiers_core"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
)

func TestFileJournalPersistsStepsAndIdempotencyResponses(t *testing.T) {
	dir := t.TempDir()
	journal, err := NewFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	if err := journal.RecordStep(ctx, "../run/one", "../../step", []byte(`"saved"`)); err != nil {
		t.Fatalf("record durable step: %v", err)
	}
	claim, err := journal.Claim(ctx, "request-key", "request-hash", time.Minute)
	if err != nil {
		t.Fatalf("claim idempotency key: %v", err)
	}
	if claim.State != action.IdempotencyClaimAcquired || claim.Token == "" {
		t.Fatalf("claim = %#v, want acquired claim with token", claim)
	}
	entry := action.IdempotencyEntry{
		Status:      200,
		Body:        []byte(`"response"`),
		RequestHash: "request-hash",
		StoredAt:    time.Now(),
	}
	if err := journal.Complete(ctx, "request-key", claim.Token, entry, time.Hour); err != nil {
		t.Fatalf("complete idempotency claim: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close journal: %v", err)
	}

	journal, err = NewFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	got, found, err := journal.GetStep(ctx, "../run/one", "../../step")
	if err != nil || !found || string(got) != `"saved"` {
		t.Fatalf("GetStep = %q, %t, %v; want saved record", got, found, err)
	}
	gotEntry, found := journal.Get(ctx, "request-key")
	if !found || string(gotEntry.Body) != `"response"` {
		t.Fatalf("Get idempotency response = %#v, %t", gotEntry, found)
	}
	completed, err := journal.Claim(ctx, "request-key", "request-hash", time.Minute)
	if err != nil || completed.State != action.IdempotencyClaimCompleted {
		t.Fatalf("replayed idempotency claim = %#v, %v", completed, err)
	}
	conflict, err := journal.Claim(ctx, "request-key", "different-hash", time.Minute)
	if err != nil || conflict.State != action.IdempotencyClaimConflict {
		t.Fatalf("mismatched idempotency claim = %#v, %v", conflict, err)
	}
}

func TestExecuteDurableJournalReplaysLoopOccurrences(t *testing.T) {
	var calls atomic.Int32
	bump := action.New("test.bump", func(_ context.Context, input any) (any, error) {
		values, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input type %T, want map[string]any", input)
		}
		var n float64
		switch value := values["n"].(type) {
		case int:
			n = float64(value)
		case float64:
			n = value
		default:
			return nil, fmt.Errorf("n type %T, want int or float64", values["n"])
		}
		calls.Add(1)
		return map[string]any{"n": n + 1}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "test-actions",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{bump}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{bundle, loop.Bundle(nil), modifiers_core.Bundle(nil), runtimeext.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	cfg.Journal = journal
	cfg.IdempotencyStore = journal

	source := "loop(test.bump:idempotent) until(.n >= 3)"
	ctx := xctx.WithRequestID(t.Context(), "durable-loop-request")
	ctx = xctx.WithExecutionID(ctx, "durable-loop-execution")
	first, err := Execute(ctx, cfg, source, "durable-loop.nflow", map[string]any{"n": 0})
	if err != nil {
		t.Fatalf("first execution: %v", err)
	}
	if first.ExecutionID != "durable-loop-execution" {
		t.Fatalf("execution ID = %q", first.ExecutionID)
	}
	firstOutput, ok := first.Output.(map[string]any)
	if !ok {
		t.Fatalf("first output = %#v, want a map", first.Output)
	}
	if output := firstOutput["n"]; output != float64(3) {
		t.Fatalf("first output n = %#v, want 3", output)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("first run invoked body %d times, want 3", got)
	}

	second, err := Execute(ctx, cfg, source, "durable-loop.nflow", map[string]any{"n": 0})
	if err != nil {
		t.Fatalf("replay execution: %v", err)
	}
	secondOutput, ok := second.Output.(map[string]any)
	if !ok {
		t.Fatalf("replayed output = %#v, want a map", second.Output)
	}
	if output := secondOutput["n"]; output != float64(3) {
		t.Fatalf("replayed output n = %#v, want 3", output)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("replay re-invoked body: calls = %d, want 3", got)
	}

	newRunCtx := xctx.WithRequestID(t.Context(), "durable-loop-request")
	newRunCtx = xctx.WithExecutionID(newRunCtx, "durable-loop-new-execution")
	third, err := Execute(newRunCtx, cfg, source, "durable-loop.nflow", map[string]any{"n": 0})
	if err != nil {
		t.Fatalf("idempotent replay under a new execution ID: %v", err)
	}
	thirdOutput, ok := third.Output.(map[string]any)
	if !ok {
		t.Fatalf("idempotent replay output = %#v, want a map", third.Output)
	}
	if output := thirdOutput["n"]; output != float64(3) {
		t.Fatalf("idempotent replay output n = %#v, want 3", output)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("idempotent replay re-invoked body: calls = %d, want 3", got)
	}
}

func TestRunnerIdempotentModifierUsesConfiguredKernelStore(t *testing.T) {
	var calls atomic.Int32
	effect := action.New("test.effect", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return fmt.Sprintf("done:%v", input), nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "test-actions",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{effect}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{bundle, modifiers_core.Bundle(nil), runtimeext.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	cfg.Journal = journal
	cfg.IdempotencyStore = journal

	source := "test.effect:idempotent"
	firstCtx := xctx.WithExecutionID(t.Context(), "idempotency-run-1")
	firstCtx = xctx.WithRequestID(firstCtx, "request-42")
	first, err := Execute(firstCtx, cfg, source, "idempotency.nflow", map[string]any{"value": "same"})
	if err != nil {
		t.Fatalf("first execution: %v", err)
	}
	if first.Output != "done:map[value:same]" {
		t.Fatalf("first output = %#v", first.Output)
	}

	secondCtx := xctx.WithExecutionID(t.Context(), "idempotency-run-2")
	secondCtx = xctx.WithRequestID(secondCtx, "request-42")
	second, err := Execute(secondCtx, cfg, source, "idempotency.nflow", map[string]any{"value": "same"})
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if second.Output != first.Output || calls.Load() != 1 {
		t.Fatalf("second output/calls = %#v/%d; want replay of %#v with one call", second.Output, calls.Load(), first.Output)
	}

	conflictCtx := xctx.WithExecutionID(t.Context(), "idempotency-run-3")
	conflictCtx = xctx.WithRequestID(conflictCtx, "request-42")
	_, err = Execute(conflictCtx, cfg, source, "idempotency.nflow", map[string]any{"value": "different"})
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("reused key with changed payload error = %v, want conflict", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("conflicting request executed action: calls = %d", calls.Load())
	}
}

func TestRunnerIdempotencyStoreIsScopedPerActionCallsite(t *testing.T) {
	var calls atomic.Int32
	effect := action.New("test.effect", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return fmt.Sprintf("done:%v", input), nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "test-actions",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{effect}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{bundle, modifiers_core.Bundle(nil), runtimeext.Bundle(nil), syntax.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg.IdempotencyStore = store

	source := "test.effect:idempotent\n-> test.effect:idempotent"
	firstCtx := xctx.WithRequestID(t.Context(), "shared-workflow-request")
	first, err := Execute(firstCtx, cfg, source, "two-steps.nflow", map[string]any{"value": "same"})
	if err != nil {
		t.Fatalf("first execution: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("first execution called %d times, want 2", calls.Load())
	}

	secondCtx := xctx.WithRequestID(t.Context(), "shared-workflow-request")
	second, err := Execute(secondCtx, cfg, source, "two-steps.nflow", map[string]any{"value": "same"})
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if second.Output != first.Output || calls.Load() != 2 {
		t.Fatalf("replay output/calls = %#v/%d; want %#v and two calls", second.Output, calls.Load(), first.Output)
	}
}

func TestExecuteGeneratesAndReturnsExecutionID(t *testing.T) {
	noop := action.New("test.noop", func(_ context.Context, input any) (any, error) { return input, nil }).Build()
	bundle := compiler.Bundle{
		ID:        "test-actions",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{noop}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{bundle, runtimeext.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), cfg, "test.noop", "generated-id.nflow", map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ExecutionID) != len("flow-")+32 || result.ExecutionID[:len("flow-")] != "flow-" {
		t.Fatalf("generated execution ID = %q, want flow- plus 32 hex characters", result.ExecutionID)
	}
}
