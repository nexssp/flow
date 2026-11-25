package events

import (
	"context"
	"fmt"
	"reflect"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// NonRetainingEventSink is the synchronous route contract for typed event sinks.
// Implementations must not retain or mutate the payload or context after the
// call returns. PayloadType must be the exact pointer type accepted by HandleEvent.
// Event sinks are invoked directly, outside action execution and journal steps.
type NonRetainingEventSink interface {
	EventPayloadType() reflect.Type
	HandleEvent(context.Context, any) error
}

// NonRetainingEventSinkAction is both a Catalog-mountable action and a typed
// synchronous event sink.
type NonRetainingEventSinkAction interface {
	action.AnyAction
	NonRetainingEventSink
}

type typedEventSinkAction[T any] struct {
	action.AnyAction
	handle func(context.Context, *T) error
}

// NewNonRetainingEventSink creates a Catalog-resolvable typed sink action. Its
// route path calls handler directly with the original caller-owned *T payload;
// it does not project fields, coerce the payload, or run the action's journal
// lifecycle. The handler must complete synchronously and must not retain or
// mutate the payload or context.
func NewNonRetainingEventSink[T any](name string, handler func(context.Context, *T) error) NonRetainingEventSinkAction {
	if handler == nil {
		panic("events: non-retaining sink handler is nil")
	}
	payloadType := reflect.TypeFor[T]()
	if payloadType.Kind() != reflect.Struct {
		panic(fmt.Sprintf("events: sink payload type must be a struct, got %s", payloadType))
	}
	base := action.New(name, func(ctx context.Context, payload *T) (any, error) {
		return nil, handler(ctx, payload)
	}).Build()
	return &typedEventSinkAction[T]{AnyAction: base, handle: handler}
}

func (sink *typedEventSinkAction[T]) EventPayloadType() reflect.Type {
	return reflect.TypeFor[*T]()
}

func (sink *typedEventSinkAction[T]) HandleEvent(ctx context.Context, payload any) error {
	typed, ok := payload.(*T)
	if !ok || typed == nil {
		return xerr.Validation(fmt.Sprintf("event sink %s: payload type is %T, want %s", sink.Describe().Name, payload, reflect.TypeFor[*T]()))
	}
	return sink.handle(ctx, typed)
}

// CloneWithHooks preserves the typed sink contract when the runner clones its
// Catalog-visible action during resolver setup.
func (sink *typedEventSinkAction[T]) CloneWithHooks(hooks ...action.AnyHook) action.AnyAction {
	return &typedEventSinkAction[T]{
		AnyAction: sink.AnyAction.CloneWithHooks(hooks...),
		handle:    sink.handle,
	}
}
