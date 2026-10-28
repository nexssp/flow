package contracts

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest"
)

func TestReadySignalDoesNotOpenAfterServiceStops(t *testing.T) {
	signal := NewReadySignal()
	signal.MarkStopped()
	signal.MarkReady()
	select {
	case <-signal.Done():
		t.Fatal("readiness was signaled after the service stopped")
	default:
	}
}

func TestReadySignalIsOneShot(t *testing.T) {
	signal := NewReadySignal()
	signal.MarkReady()
	signal.MarkReady()
	select {
	case <-signal.Done():
	default:
		t.Fatal("readiness channel was not closed")
	}
}

func TestSignalReadyOutsideSupervisorIsNoop(_ *testing.T) {
	SignalReady(context.Background())
}

func TestSignalReadySteadyStateDoesNotAllocate(t *testing.T) {
	signal := NewReadySignal()
	ctx := WithServiceReady(context.Background(), signal)
	SignalReady(ctx) // warm up the one-shot path before measuring

	xtest.RequireZeroAlloc(t, 1000, func() { SignalReady(ctx) })
}
