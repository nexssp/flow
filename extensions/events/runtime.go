package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

const compiledRoutesMetadataKey = "flow.event.compiled_routes"

type compiledRoute struct {
	EventName   string
	Subject     string
	BundleID    string
	PayloadType reflect.Type
	Sink        NonRetainingEventSink
}

type compiledRouteSet struct {
	bySubject map[string]compiledRoute
}

var (
	installedRoutesKey = xctx.NewKey[*compiledRouteSet]("flow.event.installed_routes")
	routeInvocationKey = xctx.NewKey[bool]("flow.event.route_invocation")
)

// WithRouteInvocation marks an action context as being used for a route sink.
// Typed event sinks are now invoked directly and do not need this marker.
func WithRouteInvocation(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return routeInvocationKey.With(ctx, true)
}

// IsRouteInvocation reports whether ctx is executing a legacy route invocation.
func IsRouteInvocation(ctx context.Context) bool {
	marked, _ := routeInvocationKey.From(ctx)
	return marked
}

// MaterializeRoutes resolves explicit event sources and direct typed sinks from
// the mounted Catalog resolver. Routed sinks must implement the non-retaining
// contract; arbitrary expressions and dynamically coerced actions are rejected.
func MaterializeRoutes(req compiler.MaterializeReq) error {
	routes := RoutesFromMeta(req.Meta)
	if len(routes) == 0 {
		return nil
	}
	if req.Meta == nil {
		return xerr.Internal("@events: compiler metadata is unavailable")
	}
	if req.Resolver == nil {
		return xerr.Internal("@events: action resolver is unavailable")
	}

	specs := make(map[string]compiler.MountedEventSpec, len(req.EventSpecs))
	for _, spec := range req.EventSpecs {
		specs[spec.Name] = spec
	}
	routeSet := &compiledRouteSet{bySubject: make(map[string]compiledRoute, len(routes))}
	for _, declaration := range routes {
		spec, ok := specs[declaration.EventName]
		if !ok {
			return compiler.SourceError(declaration.Position,
				"@events: unknown event source %q", declaration.EventName)
		}
		if spec.PayloadType == nil {
			return compiler.SourceError(declaration.Position,
				"@events: event %q has no payload type", declaration.EventName)
		}
		payloadType := spec.PayloadType
		for payloadType.Kind() == reflect.Pointer {
			payloadType = payloadType.Elem()
		}
		if payloadType.Kind() != reflect.Struct {
			return compiler.SourceError(declaration.Position,
				"@events: event %q payload must be a struct", declaration.EventName)
		}

		actionSink, exists := req.Resolver.Action(declaration.SinkName)
		if !exists || actionSink == nil {
			return compiler.SourceError(declaration.SinkPosition,
				"@events: unknown event sink %q", declaration.SinkName)
		}
		var sink NonRetainingEventSink
		for depth := 0; depth < 8 && actionSink != nil; depth++ {
			if typedSink, ok := actionSink.(NonRetainingEventSink); ok {
				sink = typedSink
				break
			}
			wrapper, ok := actionSink.(compiler.ActionWrapper)
			if !ok {
				break
			}
			actionSink = wrapper.WrappedAction()
		}
		if sink == nil {
			return compiler.SourceError(declaration.SinkPosition,
				"@events: sink %q must implement the typed non-retaining event-sink contract", declaration.SinkName)
		}
		expectedPayloadType := reflect.PointerTo(payloadType)
		if sink.EventPayloadType() != expectedPayloadType {
			return compiler.SourceError(declaration.SinkPosition,
				"@events: sink %q accepts %s; event %q publishes %s",
				declaration.SinkName, sink.EventPayloadType(), declaration.EventName, expectedPayloadType)
		}
		if _, duplicate := routeSet.bySubject[spec.Subject]; duplicate {
			return compiler.SourceError(declaration.Position,
				"@events: publisher subject %q is already routed", spec.Subject)
		}
		routeSet.bySubject[spec.Subject] = compiledRoute{
			EventName:   spec.Name,
			Subject:     spec.Subject,
			BundleID:    spec.BundleID,
			PayloadType: expectedPayloadType,
			Sink:        sink,
		}
	}
	req.Meta[compiledRoutesMetadataKey] = routeSet
	return nil
}

// InstallPublisher composes the source's explicit routes with any publisher
// supplied by the host. The host publisher receives the original payload and
// context; it may retain the caller-owned payload. No pooled or shared payload
// state is used by the route table.
func InstallPublisher(ctx context.Context, meta map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	routes, ok := meta[compiledRoutesMetadataKey].(*compiledRouteSet)
	if !ok || routes == nil || len(routes.bySubject) == 0 {
		return ctx
	}
	if installed, ok := installedRoutesKey.From(ctx); ok && installed == routes {
		return ctx
	}
	delegate, _ := xctx.EventPublisherFrom(ctx)
	ctx = xctx.WithEventPublisher(ctx, routedPublisher{routes: routes, delegate: delegate})
	return installedRoutesKey.With(ctx, routes)
}

// PublishFromBundle publishes a caller-owned typed event through the publisher
// injected with Kernel's xctx.EventPublisher seam. Payload must point to an
// immutable event value that the caller will not mutate or reuse; host delegates
// may retain it. The local route sink is synchronous and must not retain it.
func PublishFromBundle[T any](ctx context.Context, bundleID, subject string, payload *T) error {
	if payload == nil {
		return xerr.BadRequest(fmt.Sprintf("event %s: payload is nil", subject))
	}
	if ctx == nil {
		return xerr.BadRequest("event publisher context is nil")
	}
	publisher, ok := xctx.EventPublisherFrom(ctx)
	if !ok || publisher == nil {
		return xerr.Unavailable("event publisher is unavailable")
	}
	if scoped, ok := publisher.(bundleScopedPublisher); ok {
		return scoped.PublishEventFromBundle(ctx, bundleID, subject, payload)
	}
	return publisher.PublishEvent(ctx, subject, payload)
}

type bundleScopedPublisher interface {
	PublishEventFromBundle(context.Context, string, string, any) error
}

type routedPublisher struct {
	routes   *compiledRouteSet
	delegate xctx.EventPublisher
}

// PublishEvent deliberately has no bundle identity. A routed subject requires
// PublishFromBundle so producer authorization does not need a per-call context
// wrapper or an allocation.
func (publisher routedPublisher) PublishEvent(ctx context.Context, subject string, payload any) error {
	return publisher.PublishEventFromBundle(ctx, "", subject, payload)
}

func (publisher routedPublisher) PublishEventFromBundle(ctx context.Context, bundleID, subject string, payload any) error {
	return publisher.publishEventFromBundle(ctx, bundleID, subject, payload, false)
}

func (publisher routedPublisher) publishEventFromBundle(ctx context.Context, bundleID, subject string, payload any, routeHandled bool) error {
	if ctx == nil {
		return xerr.BadRequest("event publisher context is nil")
	}
	route, hasRoute := publisher.routes.bySubject[subject]
	var routeErr error
	if hasRoute {
		if bundleID == "" || bundleID != route.BundleID {
			return xerr.Forbidden(fmt.Sprintf("event route %s: producer bundle %q is not authorized; expected %q",
				route.EventName, bundleID, route.BundleID))
		}
		if reflect.TypeOf(payload) != route.PayloadType {
			return xerr.Validation(fmt.Sprintf("event route %s (bundle %s): payload type is %T, want %s",
				route.EventName, route.BundleID, payload, route.PayloadType))
		}
		routeErr = route.Sink.HandleEvent(ctx, payload)
	} else if publisher.delegate == nil && !routeHandled {
		return xerr.NotFound(fmt.Sprintf("no NFlow route or host publisher for event subject %q", subject))
	}

	var delegateErr error
	if publisher.delegate != nil {
		delegateErr = publishToDelegate(ctx, publisher.delegate, bundleID, subject, payload, routeHandled || hasRoute)
	}
	if routeErr == nil {
		return delegateErr
	}
	if delegateErr == nil {
		return routeErr
	}
	return errors.Join(routeErr, delegateErr)
}

func publishToDelegate(ctx context.Context, delegate xctx.EventPublisher, bundleID, subject string, payload any, routeHandled bool) error {
	if routed, ok := delegate.(routedPublisher); ok {
		return routed.publishEventFromBundle(ctx, bundleID, subject, payload, routeHandled)
	}
	if scoped, ok := delegate.(bundleScopedPublisher); ok {
		return scoped.PublishEventFromBundle(ctx, bundleID, subject, payload)
	}
	return delegate.PublishEvent(ctx, subject, payload)
}

// payloadExpressionBinding remains available to isolated compiler benchmarks;
// the production event route no longer creates expression maps.
func payloadExpressionBinding(payload any, projector *compiler.ExpressionProjector) (map[string]any, error) {
	if projector == nil {
		return nil, xerr.Internal("event payload projector is unavailable")
	}
	binding, err := projector.Project(payload)
	if err != nil {
		return nil, xerr.Validation("event payload projection", err)
	}
	return binding, nil
}
