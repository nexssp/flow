package realtime

import (
	"context"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/timing"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
)

// TopicKey provides the lock-free broadcast channel for the current room.
var TopicKey = xctx.NewKey[interface{ Publish(any) }]("realtime.topic")

// WheelKey provides the game loop's timing wheel.
var WheelKey = xctx.NewKey[*timing.Wheel]("realtime.wheel")

func TopicPublishAction() action.AnyAction {
	return action.New("realtime.topic.publish", func(ctx context.Context, req any) (any, error) {
		topic, ok := TopicKey.From(ctx)
		if !ok || topic == nil {
			return nil, xerr.Internal("realtime.topic.publish: no topic in context")
		}
		topic.Publish(req)
		return req, nil
	}).Description("Publish payload to the room's lock-free broadcast topic").
		Tag("realtime", "network").
		Build()
}

type WheelScheduleReq struct {
	DelayMs int64 `json:"delay_ms"`
	Payload any   `json:"payload"`
}

type WheelScheduleRes struct {
	TimerID uint64 `json:"timer_id"`
}

func WheelScheduleAction() action.AnyAction {
	return action.New("realtime.wheel.schedule", func(ctx context.Context, req WheelScheduleReq) (WheelScheduleRes, error) {
		wheel, ok := WheelKey.From(ctx)
		if !ok || wheel == nil {
			return WheelScheduleRes{}, xerr.Internal("realtime.wheel.schedule: no wheel in context")
		}
		if req.DelayMs <= 0 {
			return WheelScheduleRes{}, xerr.BadRequest("delay_ms must be > 0")
		}

		delay := time.Duration(req.DelayMs) * time.Millisecond
		id, err := wheel.Schedule(delay, req.Payload)
		if err != nil {
			return WheelScheduleRes{}, xerr.Unavailable("wheel exhausted", err)
		}

		return WheelScheduleRes{TimerID: uint64(id)}, nil
	}).Description("Schedule a payload to be emitted by the wheel later").
		Tag("realtime", "timing").
		Build()
}

func WheelCancelAction() action.AnyAction {
	return action.New("realtime.wheel.cancel", func(ctx context.Context, req struct {
		TimerID uint64 `json:"timer_id"`
	},
	) (bool, error) {
		wheel, ok := WheelKey.From(ctx)
		if !ok || wheel == nil {
			return false, xerr.Internal("realtime.wheel.cancel: no wheel in context")
		}
		success := wheel.Cancel(timing.TimerID(req.TimerID))
		return success, nil
	}).Description("Cancel a scheduled wheel timer").
		Tag("realtime", "timing").
		Build()
}

func WheelAdvanceAction() action.AnyAction {
	return action.New("realtime.wheel.advance", func(ctx context.Context, req struct {
		DtMs int64 `json:"dt_ms"`
	},
	) ([]any, error) {
		wheel, ok := WheelKey.From(ctx)
		if !ok || wheel == nil {
			return nil, xerr.Internal("realtime.wheel.advance: no wheel in context")
		}
		dt := time.Duration(req.DtMs) * time.Millisecond

		// Allocate the results slice here, as this is the boundary returning to the DSL.
		return wheel.Advance(dt, nil), nil
	}).Description("Advance the timing wheel by DtMs and return expired payloads").
		Tag("realtime", "timing").
		Build()
}
