package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestFileJournalRecordStepHonorsCancellationWhileWaitingForLock(t *testing.T) {
	journal, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })

	baseCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &firstCheckContext{Context: baseCtx, checked: make(chan struct{})}

	journal.mu.Lock()
	result := make(chan error, 1)
	go func() {
		result <- journal.RecordStep(ctx, "canceled-run", "step", []byte(`"must-not-persist"`))
	}()
	<-ctx.checked // RecordStep passed its first cancellation check and is blocked on mu.
	cancel()
	journal.mu.Unlock()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("RecordStep error = %v, want context.Canceled", err)
	}
	if _, found, err := journal.GetStep(t.Context(), "canceled-run", "step"); err != nil || found {
		t.Fatalf("canceled step record found=%t, err=%v; want no persisted record", found, err)
	}
}

type firstCheckContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (ctx *firstCheckContext) Err() error {
	ctx.once.Do(func() { close(ctx.checked) })
	return ctx.Context.Err()
}
