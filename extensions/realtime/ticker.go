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
		maxCatchupDuration := time.Duration(req.MaxCatchupMs) * time.Millisecond
		if maxCatchupDuration <= 0 {
			maxCatchupDuration = 100 * time.Millisecond
		}
		// Yields delta-time (dt) to feed the game loop
		return stream.Ticks(ctx, req.Hz, maxCatchupDuration), nil
	})

	meta := *src.Describe()
	meta.Description = "Emit fixed-timestep tick durations to drive a real-time loop"
	meta.Tags = []string{"realtime", "loop"}

	return &tickerMetadataStream{AnyStreamAction: src, meta: meta}
}

type tickerMetadataStream struct {
	action.AnyStreamAction
	meta action.Meta
}

var _ action.AnyStreamAction = (*tickerMetadataStream)(nil)

func (s *tickerMetadataStream) Describe() *action.Meta {
	if s == nil {
		return nil
	}
	meta := cloneTickerMeta(s.meta)
	return &meta
}

func (s *tickerMetadataStream) CloneWithHooks(hooks ...action.AnyHook) action.AnyStreamAction {
	if s == nil || s.AnyStreamAction == nil {
		return nil
	}
	inner := s.AnyStreamAction.CloneWithHooks(hooks...)
	if inner == nil {
		return nil
	}
	return &tickerMetadataStream{
		AnyStreamAction: inner,
		meta:            cloneTickerMeta(s.meta),
	}
}

func cloneTickerMeta(meta action.Meta) action.Meta {
	meta.Tags = append([]string(nil), meta.Tags...)
	meta.RequiredRoles = append([]string(nil), meta.RequiredRoles...)
	meta.RequiredPermissions = append([]string(nil), meta.RequiredPermissions...)
	meta.RequiredFeatures = append([]string(nil), meta.RequiredFeatures...)
	return meta
}
