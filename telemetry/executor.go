package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/transport/codec"
)

type ActionInvoker interface {
	ExecuteDecoded(ctx context.Context, decodeFn func(any) error) (any, error)
}

type HotPathExecutor struct {
	ring *LockFreeRingBuffer
}

func NewHotPathExecutor(ring *LockFreeRingBuffer) *HotPathExecutor {
	return &HotPathExecutor{ring: ring}
}

func (e *HotPathExecutor) ExecuteNode(
	ctx context.Context,
	nodeID [16]byte,
	invoker ActionInvoker,
	payloadInput []byte,
) (out any, err error) {
	start := time.Now().UnixNano()
	status := uint16(200)

	defer func() {
		if r := recover(); r != nil {
			status = 500
			err = fmt.Errorf("hotpath: panic isolated in node execution: %v", r)
		}

		if e.ring != nil {
			e.ring.Push(nodeID, time.Now().UnixNano()-start, 0, status)
		}
	}()

	select {
	case <-ctx.Done():
		status = 504

		return nil, ctx.Err()
	default:
	}

	if invoker == nil {
		status = 400

		return nil, xerr.BadRequest("hotpath: nil action invoker")
	}

	out, execErr := invoker.ExecuteDecoded(ctx, func(target any) error {
		if target == nil || len(payloadInput) == 0 {
			return nil
		}

		return codec.Default.Unmarshal(payloadInput, target)
	})
	if execErr != nil {
		status = 500

		return nil, execErr
	}

	return out, nil
}
