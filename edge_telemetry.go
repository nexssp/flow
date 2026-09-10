package flow

import (
	"sync/atomic"
	"unsafe"
)

const EdgeRingSize = 1024
const edgeRingMask = EdgeRingSize - 1

type EdgeEventSlot struct {
	Seq        uint64
	LatencyNs  int64
	StatusCode uint16
	_          [6]byte
	EdgeID     [16]byte
	_          [24]byte
}

type paddedEdgeSeq struct {
	val atomic.Uint64
	_   [56]byte
}

type EdgeTelemetryRing struct {
	head     atomic.Uint64
	_        [56]byte
	tail     atomic.Uint64
	_        [56]byte
	buffer   [EdgeRingSize]EdgeEventSlot
	sequence [EdgeRingSize]paddedEdgeSeq
}

func NewEdgeTelemetryRing() *EdgeTelemetryRing {
	r := &EdgeTelemetryRing{}
	for i := range r.sequence {
		r.sequence[i].val.Store(uint64(i))
	}

	return r
}

func (r *EdgeTelemetryRing) Push(edgeID [16]byte, latencyNs int64, statusCode uint16) bool {
	var head uint64
	for {
		head = r.head.Load()
		seq := r.sequence[head&edgeRingMask].val.Load()

		if seq == head {
			if r.head.CompareAndSwap(head, head+1) {
				break
			}
		} else if seq < head {
			return false
		}
	}

	slot := &r.buffer[head&edgeRingMask]
	slot.Seq = head
	slot.LatencyNs = latencyNs
	slot.StatusCode = statusCode
	slot.EdgeID = edgeID

	r.sequence[head&edgeRingMask].val.Store(head + 1)

	return true
}

func (r *EdgeTelemetryRing) BatchDrain(dst []EdgeEventSlot) int {
	drained := 0
	for drained < len(dst) {
		var tail uint64
		for {
			tail = r.tail.Load()
			next := tail + 1
			seq := r.sequence[tail&edgeRingMask].val.Load()

			if seq == next {
				if r.tail.CompareAndSwap(tail, next) {
					break
				}
			} else if seq < next {
				return drained
			}
		}

		dst[drained] = r.buffer[tail&edgeRingMask]
		r.sequence[tail&edgeRingMask].val.Store(tail + edgeRingMask + 1)

		drained++
	}

	return drained
}

var _ = unsafe.Sizeof(EdgeTelemetryRing{})
