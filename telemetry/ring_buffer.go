package telemetry

import (
	"sync/atomic"
	"unsafe"
)

const (
	RingCapacity = 1024
	ringMask     = RingCapacity - 1
)

type EventSlot struct {
	Seq        uint64
	Timestamp  int64
	Duration   int64
	CostMicros int64
	Status     uint16
	_          [6]byte
	NodeID     [16]byte
	_          [8]byte
}

type paddedSeq struct {
	val atomic.Uint64
	_   [56]byte
}

type LockFreeRingBuffer struct {
	head     atomic.Uint64
	_        [56]byte
	tail     atomic.Uint64
	_        [56]byte
	slots    [RingCapacity]EventSlot
	sequence [RingCapacity]paddedSeq
}

func NewLockFreeRingBuffer() *LockFreeRingBuffer {
	rb := &LockFreeRingBuffer{}
	for i := uint64(0); i < RingCapacity; i++ {
		rb.sequence[i].val.Store(i)
	}
	return rb
}

func (rb *LockFreeRingBuffer) Push(nodeID [16]byte, duration, cost int64, status uint16) bool {
	var head uint64
	for {
		head = rb.head.Load()
		seq := rb.sequence[head&ringMask].val.Load()

		if seq == head {
			if rb.head.CompareAndSwap(head, head+1) {
				break
			}
		} else if seq < head {
			return false
		}
	}

	slot := &rb.slots[head&ringMask]
	slot.Seq = head
	slot.Duration = duration
	slot.CostMicros = cost
	slot.Status = status
	slot.NodeID = nodeID

	rb.sequence[head&ringMask].val.Store(head + 1)
	return true
}

func (rb *LockFreeRingBuffer) Pop(dst *EventSlot) bool {
	var tail uint64
	for {
		tail = rb.tail.Load()
		next := tail + 1
		seq := rb.sequence[tail&ringMask].val.Load()

		if seq == next {
			if rb.tail.CompareAndSwap(tail, next) {
				break
			}
		} else if seq < next {
			return false
		}
	}

	*dst = rb.slots[tail&ringMask]
	rb.sequence[tail&ringMask].val.Store(tail + ringMask + 1)
	return true
}

var _ = unsafe.Sizeof(LockFreeRingBuffer{})
