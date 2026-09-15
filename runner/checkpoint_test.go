package runner

import (
	"context"
	"testing"
	"time"
)

func TestCheckpointStoreContract(t *testing.T) {
	t.Run("FileCheckpointStore", func(t *testing.T) {
		s, err := NewFileCheckpointStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		runCheckpointStoreContract(t, s)
	})
	t.Run("MemoryCheckpointStore", func(t *testing.T) {
		runCheckpointStoreContract(t, NewMemoryCheckpointStore())
	})
}

func runCheckpointStoreContract(t *testing.T, s CheckpointStore) {
	t.Helper()

	ctx := context.Background()

	cp := Checkpoint{
		RunID:    "run_123",
		Flow:     "foo.flow",
		FlowHash: "abc123",
		Layer:    2,
		SavedAt:  time.Now().UTC(),
		State: map[string]any{
			"tasks.a.output": "alpha",
			"tasks.b.output": float64(42),
		},
	}

	if _, found, err := s.Load(ctx, cp.RunID); err != nil || found {
		t.Fatalf("Load before Save: found=%v err=%v", found, err)
	}

	if err := s.Save(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, found, err := s.Load(ctx, cp.RunID)
	if err != nil || !found {
		t.Fatalf("Load after Save: found=%v err=%v", found, err)
	}

	if got.RunID != cp.RunID || got.Layer != cp.Layer || got.FlowHash != cp.FlowHash {
		t.Fatalf("checkpoint fields lost: %+v", got)
	}

	if got.State["tasks.a.output"] != "alpha" {
		t.Fatalf("state lost: %+v", got.State)
	}

	if err := s.Delete(ctx, cp.RunID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, found, err := s.Load(ctx, cp.RunID); err != nil || found {
		t.Fatalf("Load after Delete: found=%v err=%v", found, err)
	}
}

func TestFlowHash_Stable(t *testing.T) {
	first := flowHash("a -> b")
	second := flowHash("a -> b")

	if first != second {
		t.Fatalf("hash not stable across calls: %q != %q", first, second)
	}

	if flowHash("a -> b") == flowHash("a -> c") {
		t.Fatal("hash must differ for different DSL")
	}
}
