package telemetry_test

import (
	"sync"
	"testing"

	"github.com/nexssp/flow/telemetry"
)

func TestLockFreeRingBuffer_PushAndPop(t *testing.T) {
	t.Parallel()

	rb := telemetry.NewLockFreeRingBuffer()
	var nodeID [16]byte
	copy(nodeID[:], "node_test_01")

	if !rb.Push(nodeID, 120, 50, 200) {
		t.Fatal("expected push to succeed")
	}

	var slot telemetry.EventSlot
	if !rb.Pop(&slot) {
		t.Fatal("expected pop to succeed")
	}

	if slot.Duration != 120 || slot.CostMicros != 50 || slot.Status != 200 {
		t.Fatalf("unexpected popped slot: %+v", slot)
	}

	if rb.Pop(&slot) {
		t.Fatal("expected empty pop to return false")
	}
}

func TestLockFreeRingBuffer_ConcurrentContention(t *testing.T) {
	t.Parallel()

	rb := telemetry.NewLockFreeRingBuffer()
	var wg sync.WaitGroup

	const producers = 8
	const perProducer = 100

	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var rawID [16]byte
			rawID[0] = byte(id & 0xFF)

			for i := 0; i < perProducer; i++ {
				_ = rb.Push(rawID, int64(i), 0, 200)
			}
		}(p)
	}

	wg.Wait()

	drained := 0
	var slot telemetry.EventSlot
	for rb.Pop(&slot) {
		drained++
	}

	if drained != producers*perProducer {
		t.Fatalf("expected %d items drained, got %d", producers*perProducer, drained)
	}
}
