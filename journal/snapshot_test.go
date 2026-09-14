package journal_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/flow/journal"
)

func TestFileSnapshotJournal_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	j, err := journal.NewFileSnapshotJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	snap := journal.Snapshot{
		RunID:       "run_001",
		StepIndex:   3,
		SpentMicros: 4200,
		Timestamp:   time.Now().UTC(),
		StateData:   map[string]any{"key": "value", "count": 7},
	}
	if err := j.Save(ctx, snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, found, err := j.Recover(ctx, "run_001")
	if err != nil {
		t.Fatalf("recover: %v", err)
	}

	if !found {
		t.Fatal("expected found=true")
	}

	if got.StepIndex != 3 || got.SpentMicros != 4200 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}

	if v, ok := got.StateData["key"].(string); !ok || v != "value" {
		t.Fatalf("state_data key mismatch: %#v", got.StateData)
	}
}

func TestFileSnapshotJournal_OverwriteWithLatestStep(t *testing.T) {
	dir := t.TempDir()

	j, err := journal.NewFileSnapshotJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if err := j.Save(ctx, journal.Snapshot{
			RunID:     "run_002",
			StepIndex: i,
			Timestamp: time.Now().UTC(),
			StateData: map[string]any{"step": i},
		}); err != nil {
			t.Fatalf("save step %d: %v", i, err)
		}
	}

	got, found, err := j.Recover(ctx, "run_002")
	if err != nil || !found {
		t.Fatalf("recover: found=%v err=%v", found, err)
	}
	// Recover returns the most recent snapshot via latest.json
	if got.StepIndex != 3 {
		t.Fatalf("expected step 3, got %d", got.StepIndex)
	}
}

func TestFileSnapshotJournal_MissingReturnsNotFound(t *testing.T) {
	dir := t.TempDir()

	j, err := journal.NewFileSnapshotJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, found, err := j.Recover(context.Background(), "no_such_run")
	if err != nil {
		t.Fatalf("recover: %v", err)
	}

	if found {
		t.Fatal("expected found=false")
	}
}
