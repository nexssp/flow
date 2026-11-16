package realtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/broadcast"
	"github.com/nexssp/kernel/timing"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/realtime"
)

func TestRealtime_TickerSourceMetadataSurvivesDescribeAndClone(t *testing.T) {
	t.Parallel()

	source := realtime.TickerSource()
	requireTickerMetadata(t, source)

	meta := source.Describe()
	meta.Tags[0] = "changed"
	requireTickerMetadata(t, source)

	clone := source.CloneWithHooks()
	requireTickerMetadata(t, clone)
	clone.Describe().Tags[1] = "changed"
	requireTickerMetadata(t, clone)
	requireTickerMetadata(t, source)
}

func requireTickerMetadata(t *testing.T, source action.AnyStreamAction) {
	t.Helper()
	meta := source.Describe()
	if meta == nil {
		t.Fatal("ticker metadata is nil")
	}
	if got, want := meta.Description, "Emit fixed-timestep tick durations to drive a real-time loop"; got != want {
		t.Fatalf("ticker description = %q, want %q", got, want)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "realtime" || meta.Tags[1] != "loop" {
		t.Fatalf("ticker tags = %#v, want [realtime loop]", meta.Tags)
	}
}

func TestRealtime_TopicPublish(t *testing.T) {
	t.Parallel()

	topic := broadcast.NewTopic[any](16)
	ctx := realtime.TopicKey.With(context.Background(), topic)

	bundle := realtime.Bundle(nil)
	resolver, _ := compiler.NewDynamicResolver(bundle.Libraries...)

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
	resolver, _ := compiler.NewDynamicResolver(bundle.Libraries...)

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
	unexpired, ok := res.([]any)
	if !ok {
		t.Fatalf("advance result has type %T, want []any", res)
	}
	ktest.RequireEqual(t, len(unexpired), 0)

	// Advance remaining 30ms -> should expire
	res, err = action.InvokeAny(ctx, advanceAction, map[string]any{"dt_ms": 30})
	ktest.RequireNoError(t, err)
	expired, ok := res.([]any)
	if !ok {
		t.Fatalf("advance result has type %T, want []any", res)
	}
	ktest.RequireEqual(t, len(expired), 1)
	ktest.RequireEqual(t, expired[0], "buff_expired")
}
