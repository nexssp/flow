package stream_ops

import (
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"
)

// Operators exposes the stateless stream manipulators into the DSL.
func Operators() []action.NamedOperator {
	return []action.NamedOperator{
		action.NewOperator(
			"stream.batch",
			func(cfg struct {
				Size int `json:"size" validate:"required"`
			},
			) action.StreamOp[any, []any] {
				return stream.Batch[any](cfg.Size)
			},
		),
		action.NewOperator(
			"stream.throttle",
			func(cfg struct {
				IntervalMs int64 `json:"interval_ms" validate:"required"`
			},
			) action.StreamOp[any, any] {
				return stream.Throttle[any](time.Duration(cfg.IntervalMs) * time.Millisecond)
			},
		),
		action.NewOperator(
			"stream.window",
			func(cfg struct {
				Size int `json:"size" validate:"required"`
				Step int `json:"step" validate:"required"`
			},
			) action.StreamOp[any, []any] {
				return stream.Window[any](cfg.Size, cfg.Step)
			},
		),
		action.NewOperator(
			"stream.take",
			func(cfg struct {
				Count int `json:"count" validate:"required"`
			},
			) action.StreamOp[any, any] {
				return stream.Take[any](cfg.Count)
			},
		),
		action.NewOperator(
			"stream.collect",
			func(cfg struct {
				Limit int `json:"limit" validate:"required"`
			},
			) action.StreamOp[any, []any] {
				return stream.Collect[any](cfg.Limit)
			},
		),
	}
}
