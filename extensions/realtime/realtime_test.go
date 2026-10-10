package realtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/broadcast"
	"github.com/nexssp/kernel/timing"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/realtime"
)

func TestRealtime_TopicPublish(t *testing.T) {
	t.Parallel()

	topic := broadcast.NewTopic[any](16)
	ctx := realtime.TopicKey.With(context.Background(), topic)

	bundle := realtime.Bundle(nil)
	resolver, _ := core.NewDynamicResolver(bundle.Libraries...)

	pubAction, ok := resolver.Action("realtime.topic.publish")
	ktest.RequireTrue(t, ok)

	res, err := action.InvokeAny(ctx, pubAction, "game_state_payload")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res, "game_state_payload")

	var cursor broadcast.Cursor
	dst := make([]any, 2)
	n := topic.Read(&cursor, dst)
	ktest.RequireEqual(t, n, 1)
	ktest.RequireEqual(t, dst[0], "game_state_payload")
}

func TestRealtime_WheelScheduleAndAdvance(t *testing.T) {
	t.Parallel()

	wheel := timing.New(10*time.Millisecond, 64, 256)
	ctx := realtime.WheelKey.With(context.Background(), wheel)

	bundle := realtime.Bundle(nil)
	resolver, _ := core.NewDynamicResolver(bundle.Libraries...)

	schedAction, _ := resolver.Action("realtime.wheel.schedule")
	advanceAction, _ := resolver.Action("realtime.wheel.advance")

	_, err := action.InvokeAny(ctx, schedAction, map[string]any{
		"delay_ms": 30,
		"payload":  "buff_expired",
	})
	ktest.RequireNoError(t, err)

	// Advance only 10ms -> should not expire
	res, err := action.InvokeAny(ctx, advanceAction, map[string]any{"dt_ms": 10})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(res.([]any)), 0)

	// Advance remaining 30ms -> should expire
	res, err = action.InvokeAny(ctx, advanceAction, map[string]any{"dt_ms": 30})
	ktest.RequireNoError(t, err)
	expired := res.([]any)
	ktest.RequireEqual(t, len(expired), 1)
	ktest.RequireEqual(t, expired[0], "buff_expired")
}
