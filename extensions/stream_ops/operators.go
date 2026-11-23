package stream_ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"
	"github.com/nexssp/kernel/xerr"
)

// Resource limits cap configured item counts, accounted buffer bytes, and
// millisecond conversion within time.Duration's representable range.
const (
	maxStreamBufferSize         = 1_000_000
	maxStreamBufferBytes  int64 = 64 << 20
	maxThrottleIntervalMs int64 = 9_223_372_036_854
)

type batchConfig struct {
	Size     int   `json:"size"`
	MaxBytes int64 `json:"max_bytes"`
}

type throttleConfig struct {
	IntervalMs int64 `json:"interval_ms"`
}

type windowConfig struct {
	Size     int   `json:"size"`
	Step     int   `json:"step"`
	MaxBytes int64 `json:"max_bytes"`
}

type collectConfig struct {
	Limit    int   `json:"limit"`
	MaxBytes int64 `json:"max_bytes"`
}

type takeConfig struct {
	Count int `json:"count"`
}

// Operators exposes the stateless stream manipulators into the DSL.
func Operators() []action.NamedOperator {
	return []action.NamedOperator{
		action.NewOperator("stream.map", Map),
		action.NewOperator("stream.filter", Filter),
		action.NewOperator("stream.flatMap", FlatMap),
		validatedOperator("stream.batch", func(cfg batchConfig) stream.StreamOp[any, []any] {
			return batchWithBudget(cfg.Size, configuredBufferBytes(cfg.MaxBytes))
		}, func(cfg batchConfig) error {
			if err := validateBufferSize("size", cfg.Size); err != nil {
				return err
			}
			return validateBufferBytes(cfg.MaxBytes)
		}),
		action.NewOperator("stream.fold", Fold),
		validatedOperator("stream.throttle", func(cfg throttleConfig) stream.StreamOp[any, any] {
			return stream.Throttle[any](time.Duration(cfg.IntervalMs) * time.Millisecond)
		}, func(cfg throttleConfig) error {
			if cfg.IntervalMs <= 0 || cfg.IntervalMs > maxThrottleIntervalMs {
				return fmt.Errorf("interval_ms must be between 1 and %d", maxThrottleIntervalMs)
			}
			return nil
		}),
		validatedOperator("stream.window", func(cfg windowConfig) stream.StreamOp[any, []any] {
			return windowWithBudget(cfg.Size, cfg.Step, configuredBufferBytes(cfg.MaxBytes))
		}, func(cfg windowConfig) error {
			if err := validateBufferSize("size", cfg.Size); err != nil {
				return err
			}
			if err := validateBufferSize("step", cfg.Step); err != nil {
				return err
			}
			return validateBufferBytes(cfg.MaxBytes)
		}),
		validatedOperator("stream.take", func(cfg takeConfig) stream.StreamOp[any, any] {
			return stream.Take[any](cfg.Count)
		}, func(cfg takeConfig) error {
			if cfg.Count <= 0 {
				return errors.New("count must be positive")
			}
			return nil
		}),
		validatedOperator("stream.collect", func(cfg collectConfig) stream.StreamOp[any, []any] {
			return collectWithBudget(cfg.Limit, configuredBufferBytes(cfg.MaxBytes))
		}, func(cfg collectConfig) error {
			if err := validateBufferSize("limit", cfg.Limit); err != nil {
				return err
			}
			return validateBufferBytes(cfg.MaxBytes)
		}),
	}
}

func validateBufferSize(name string, size int) error {
	if size < 1 || size > maxStreamBufferSize {
		return fmt.Errorf("%s must be between 1 and %d", name, maxStreamBufferSize)
	}
	return nil
}

func validateBufferBytes(size int64) error {
	if size < 0 || size > maxStreamBufferBytes {
		return fmt.Errorf("max_bytes must be 0 (the default) or between 1 and %d", maxStreamBufferBytes)
	}
	return nil
}

func configuredBufferBytes(size int64) uint64 {
	if size < 0 {
		return 0
	}
	if size == 0 {
		return uint64(maxStreamBufferBytes)
	}
	return uint64(size)
}

func batchWithBudget(size int, maxBytes uint64) stream.StreamOp[any, []any] {
	return func(up iter.Seq2[any, error]) iter.Seq2[[]any, error] {
		return func(yield func([]any, error) bool) {
			batch := make([]any, 0)
			var used uint64
			for item, itemErr := range up {
				if itemErr != nil {
					if len(batch) > 0 && !yield(batch, nil) {
						return
					}
					yield(nil, itemErr)
					return
				}
				copied, accounted, err := prepareStreamItem(item, maxBytes-used)
				if err != nil {
					if len(batch) > 0 && !yield(batch, nil) {
						return
					}
					yield(nil, streamBufferError("stream.batch", err))
					return
				}
				batch = append(batch, copied)
				used += accounted
				if len(batch) == size {
					if !yield(batch, nil) {
						return
					}
					batch = make([]any, 0)
					used = 0
				}
			}
			if len(batch) > 0 {
				yield(batch, nil)
			}
		}
	}
}

func collectWithBudget(limit int, maxBytes uint64) stream.StreamOp[any, []any] {
	return func(up iter.Seq2[any, error]) iter.Seq2[[]any, error] {
		return func(yield func([]any, error) bool) {
			items := make([]any, 0)
			var used uint64
			for item, itemErr := range up {
				if itemErr != nil {
					yield(nil, itemErr)
					return
				}
				if len(items) == limit {
					yield(nil, streamBufferError("stream.collect", errStreamItemLimitExceeded))
					return
				}
				copied, accounted, err := prepareStreamItem(item, maxBytes-used)
				if err != nil {
					yield(nil, streamBufferError("stream.collect", err))
					return
				}
				items = append(items, copied)
				used += accounted
			}
			yield(items, nil)
		}
	}
}

func windowCapacityDelta(oldCapacity, newCapacity int, maxBytes, capacitySlotSize uint64) (uint64, error) {
	if newCapacity < oldCapacity {
		return 0, errStreamBudgetExceeded
	}
	capacityGrowth, ok := checkedStreamLength(newCapacity - oldCapacity)
	if !ok || capacitySlotSize == 0 || capacityGrowth > maxBytes/capacitySlotSize {
		return 0, errStreamBudgetExceeded
	}
	return capacityGrowth * capacitySlotSize, nil
}

func windowWithBudget(size, step int, maxBytes uint64) stream.StreamOp[any, []any] {
	return func(up iter.Seq2[any, error]) iter.Seq2[[]any, error] {
		return func(yield func([]any, error) bool) {
			buffer := make([]any, 0)
			itemSizes := make([]uint64, 0)
			anySlotSize := uint64(reflect.TypeFor[any]().Size())
			itemSizeSlotSize := uint64(reflect.TypeFor[uint64]().Size())
			capacitySlotSize := anySlotSize + itemSizeSlotSize
			itemSizeAllowance := 2 * itemSizeSlotSize
			var used uint64
			skip := 0
			for item, itemErr := range up {
				if itemErr != nil {
					yield(nil, itemErr)
					return
				}
				if skip > 0 {
					skip--
					continue
				}
				oldCapacity := cap(buffer)
				newCapacity := oldCapacity
				if len(buffer) == oldCapacity {
					if oldCapacity == 0 {
						newCapacity = 1
					} else {
						newCapacity = min(oldCapacity*2, size)
					}
				}
				capacityDelta, err := windowCapacityDelta(oldCapacity, newCapacity, maxBytes, capacitySlotSize)
				if err != nil {
					yield(nil, streamBufferError("stream.window", err))
					return
				}
				if used > maxBytes || capacityDelta > maxBytes-used || itemSizeAllowance > maxBytes-used-capacityDelta {
					yield(nil, streamBufferError("stream.window", errStreamBudgetExceeded))
					return
				}
				copied, accounted, err := prepareStreamItem(item, maxBytes-used-capacityDelta-itemSizeAllowance)
				if err != nil {
					yield(nil, streamBufferError("stream.window", err))
					return
				}
				accounted += itemSizeAllowance
				if newCapacity > oldCapacity {
					newBuffer := make([]any, len(buffer), newCapacity)
					copy(newBuffer, buffer)
					newItemSizes := make([]uint64, len(itemSizes), newCapacity)
					copy(newItemSizes, itemSizes)
					buffer = newBuffer
					itemSizes = newItemSizes
				}
				buffer = append(buffer, copied)
				itemSizes = append(itemSizes, accounted)
				used += accounted + capacityDelta
				if len(buffer) == size {
					outputSlots := uint64(len(buffer)) * anySlotSize
					if used > maxBytes || outputSlots > maxBytes-used {
						yield(nil, streamBufferError("stream.window", errStreamBudgetExceeded))
						return
					}
					window := make([]any, len(buffer))
					copy(window, buffer)
					if !yield(window, nil) {
						return
					}
					if step >= size {
						clear(buffer)
						clear(itemSizes)
						buffer = buffer[:0]
						itemSizes = itemSizes[:0]
						used = uint64(cap(buffer))*anySlotSize + uint64(cap(itemSizes))*itemSizeSlotSize
						skip = step - size
					} else {
						copy(buffer, buffer[step:])
						clear(buffer[size-step:])
						copy(itemSizes, itemSizes[step:])
						clear(itemSizes[size-step:])
						buffer = buffer[:size-step]
						itemSizes = itemSizes[:size-step]
						used = uint64(cap(buffer))*anySlotSize + uint64(cap(itemSizes))*itemSizeSlotSize
						for _, itemSize := range itemSizes {
							used += itemSize
						}
					}
				}
			}
			if len(buffer) > 0 {
				yield(buffer, nil)
			}
		}
	}
}

func streamBufferError(operator string, err error) error {
	message := operator + ": " + err.Error()
	if errors.Is(err, errStreamBudgetExceeded) || errors.Is(err, errUnsupportedStreamValue) || errors.Is(err, errStreamItemLimitExceeded) {
		return xerr.Validation(message, err)
	}
	return fmt.Errorf("%s: %w", operator, err)
}

func validatedOperator[In, Out, Cfg any](
	name string,
	factory func(Cfg) stream.StreamOp[In, Out],
	validate func(Cfg) error,
) action.NamedOperator {
	return action.NamedOperator{
		Name:       name,
		InType:     reflect.TypeFor[In](),
		OutType:    reflect.TypeFor[Out](),
		ConfigType: reflect.TypeFor[Cfg](),
		Build: func(raw any) (action.StreamOperator, error) {
			var cfg Cfg
			if raw != nil {
				normalized, err := action.NormalizeConfigForTarget(raw, &cfg)
				if err != nil {
					return nil, fmt.Errorf("operator %q config error: %w", name, err)
				}
				data, err := json.Marshal(normalized)
				if err != nil {
					return nil, fmt.Errorf("operator %q config error: %w", name, err)
				}
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&cfg); err != nil {
					return nil, fmt.Errorf("operator %q config error: %w", name, err)
				}
			}
			if err := validate(cfg); err != nil {
				return nil, fmt.Errorf("operator %q config error: %w", name, err)
			}
			return action.NewTypedStreamOperator(name, factory(cfg)), nil
		},
	}
}
