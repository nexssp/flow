package realtime

import (
	"context"
	"iter"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"
	"github.com/nexssp/kernel/xerr"
)

type TicksReq struct {
	Hz           int `json:"hz"`
	MaxCatchupMs int `json:"max_catchup_ms"`
}

// TickerSource wraps the kernel's fixed-timestep stream.
func TickerSource() action.AnyStreamAction {
	src := action.NewStream("realtime.ticks", func(ctx context.Context, req TicksReq) (iter.Seq2[time.Duration, error], error) {
		if req.Hz <= 0 {
			return nil, xerr.BadRequest("hz must be positive")
		}
		maxDur := time.Duration(req.MaxCatchupMs) * time.Millisecond
		if maxDur <= 0 {
			maxDur = 100 * time.Millisecond
		}
		// Yields delta-time (dt) to feed the game loop
		return stream.Ticks(ctx, req.Hz, maxDur), nil
	})

	// StreamAction has no fluent builder; modify Describe() directly.
	meta := src.Describe()
	meta.Description = "Emit fixed-timestep tick durations to drive a real-time loop"
	meta.Tags = []string{"realtime", "loop"}

	return src
}
