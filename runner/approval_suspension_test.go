package runner

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/approval"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
)

const approvalSource = `@approval finance_review {
  role: finance reviewer
  reason: confirm the transfer
  expires: 1h
}
test.before -> approval.finance_review -> test.after`

func TestExecuteApprovalPausePersistsAndResumesDurableSteps(t *testing.T) {
	var beforeCalls, afterCalls atomic.Int32
	before := action.New("test.before", func(_ context.Context, input any) (any, error) {
		beforeCalls.Add(1)
		return input, nil
	}).Build()
	after := action.New("test.after", func(_ context.Context, input any) (any, error) {
		afterCalls.Add(1)
		return input, nil
	}).Build()
	cfg, err := BuildConfig([]compiler.Bundle{
		{ID: "approval-test-actions", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{before, after}}}},
		approval.Bundle(nil),
		runtime.Bundle(nil),
		syntax.Bundle(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	journalDir := t.TempDir()
	journal, err := NewFileJournal(journalDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	cfg.Journal = journal
	payload := map[string]any{"amount": 125}
	ctx := xctx.WithExecutionID(t.Context(), "approval-run-1")

	first, err := Execute(ctx, cfg, approvalSource, "approval-flow.nflow", payload)
	if !errors.Is(err, dag.ErrSuspended) {
		t.Fatalf("first execution error = %v, want suspended", err)
	}
	if first.Suspension == nil || !first.Suspension.Persisted || first.Suspension.Gate != "finance_review" {
		t.Fatalf("first suspension = %#v", first.Suspension)
	}
	var kernelSuspension *dag.SuspendError
	if !errors.As(err, &kernelSuspension) {
		t.Fatalf("execution error %v does not expose Kernel's suspend details", err)
	}
	kernelPayload, ok := kernelSuspension.Payload.(map[string]any)
	if !ok || kernelPayload["gate"] != "finance_review" || kernelPayload["role"] != "finance reviewer" || kernelPayload["reason"] != "confirm the transfer" || kernelPayload["expires"] != "1h0m0s" {
		t.Fatalf("Kernel suspension payload = %#v, want the legacy approval fields", kernelSuspension.Payload)
	}
	if _, hasType := kernelPayload["type"]; hasType {
		t.Fatalf("Kernel suspension payload unexpectedly changed legacy shape: %#v", kernelPayload)
	}
	if beforeCalls.Load() != 1 || afterCalls.Load() != 0 {
		t.Fatalf("first call counts before/after = %d/%d", beforeCalls.Load(), afterCalls.Load())
	}

	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = NewFileJournal(journalDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Journal = journal
	paused, err := Execute(ctx, cfg, approvalSource, "approval-flow.nflow", payload)
	if !errors.Is(err, dag.ErrSuspended) || paused.Suspension == nil || !paused.Suspension.Persisted {
		t.Fatalf("unapproved resume = %#v, %v; want persisted suspension", paused.Suspension, err)
	}
	if beforeCalls.Load() != 1 || afterCalls.Load() != 0 {
		t.Fatalf("unapproved resume repeated work: before/after = %d/%d", beforeCalls.Load(), afterCalls.Load())
	}

	approvedCtx := xctx.WithApprovalToken(ctx, "finance_review")
	resumed, err := Execute(approvedCtx, cfg, approvalSource, "approval-flow.nflow", payload)
	if err != nil {
		t.Fatalf("approved resume: %v", err)
	}
	if beforeCalls.Load() != 1 || afterCalls.Load() != 1 {
		t.Fatalf("approved resume call counts = %d/%d, want 1/1", beforeCalls.Load(), afterCalls.Load())
	}
	output, ok := resumed.Output.(map[string]any)
	if !ok || output["amount"] != float64(125) {
		t.Fatalf("resumed output = %#v, want JSON-normalized amount 125", resumed.Output)
	}
	_, found, err := journal.LoadApprovalSuspension(t.Context(), "approval-run-1")
	if err != nil || found {
		t.Fatalf("pending approval after successful resume = %t, %v; want cleared", found, err)
	}
}

func TestPendingApprovalRejectsChangedSourceOrInput(t *testing.T) {
	var calls atomic.Int32
	before := action.New("test.before", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return input, nil
	}).Build()
	after := action.New("test.after", func(_ context.Context, input any) (any, error) { return input, nil }).Build()
	cfg, err := BuildConfig([]compiler.Bundle{
		{ID: "approval-binding-test-actions", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{before, after}}}},
		approval.Bundle(nil),
		runtime.Bundle(nil),
		syntax.Bundle(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	cfg.Journal = journal
	ctx := xctx.WithExecutionID(t.Context(), "approval-binding-run")
	payload := map[string]any{"amount": 100}
	_, err = Execute(ctx, cfg, approvalSource, "approval-flow.nflow", payload)
	if !errors.Is(err, dag.ErrSuspended) {
		t.Fatalf("initial execution error = %v, want suspended", err)
	}

	_, err = Execute(ctx, cfg, approvalSource+"\n# changed source", "approval-flow.nflow", payload)
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("changed source error = %v, want conflict", err)
	}
	_, err = Execute(ctx, cfg, approvalSource, "approval-flow.nflow", map[string]any{"amount": 101})
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("changed input error = %v, want conflict", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("source/input mismatch ran workflow actions %d times, want 1", calls.Load())
	}
}

func TestPendingApprovalExpiryFailsClosed(t *testing.T) {
	before := action.New("test.before", func(_ context.Context, input any) (any, error) { return input, nil }).Build()
	after := action.New("test.after", func(_ context.Context, input any) (any, error) { return input, nil }).Build()
	cfg, err := BuildConfig([]compiler.Bundle{
		{ID: "approval-expiry-test-actions", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{before, after}}}},
		approval.Bundle(nil),
		runtime.Bundle(nil),
		syntax.Bundle(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	cfg.Journal = journal
	payload := map[string]any{"amount": 90}
	fingerprint, err := fingerprintApprovalRun(approvalSource, "approval-expiry.nflow", payload)
	if err != nil {
		t.Fatal(err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	record := ApprovalSuspension{
		ExecutionID: "approval-expiry-run",
		Gate:        "finance_review",
		Role:        "finance reviewer",
		Reason:      "confirm the transfer",
		SourceName:  "approval-expiry.nflow",
		SourceHash:  fingerprint.sourceHash,
		InputHash:   fingerprint.inputHash,
		SuspendedAt: expiredAt.Add(-time.Hour),
		ExpiresAt:   expiredAt,
	}
	if err := journal.SaveApprovalSuspension(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	ctx := xctx.WithExecutionID(t.Context(), "approval-expiry-run")
	ctx = xctx.WithApprovalToken(ctx, "finance_review")
	_, err = Execute(ctx, cfg, approvalSource, "approval-expiry.nflow", payload)
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("expired approval error = %v, want conflict", err)
	}
}
