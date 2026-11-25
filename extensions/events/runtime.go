package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/nexssp/kernel/action"
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
	Pipeline    action.AnyAction
	Projector   *compiler.ExpressionProjector
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

// MaterializeRoutes resolves explicit event sources and either direct typed
// sinks or declared, typed NFlow pipeline handlers from the mounted resolver.
// Arbitrary expressions and dynamically coerced actions are rejected.
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
		route, err := materializeRoute(req, declaration, specs)
		if err != nil {
			return err
		}
		if _, duplicate := routeSet.bySubject[route.Subject]; duplicate {
			return compiler.SourceError(declaration.Position,
				"@events: publisher subject %q is already routed", route.Subject)
		}
		routeSet.bySubject[route.Subject] = route
	}
	req.Meta[compiledRoutesMetadataKey] = routeSet
	return nil
}

func materializeRoute(req compiler.MaterializeReq, declaration Route, specs map[string]compiler.MountedEventSpec) (compiledRoute, error) {
	spec, ok := specs[declaration.EventName]
	if !ok {
		return compiledRoute{}, compiler.SourceError(declaration.Position,
			"@events: unknown event source %q", declaration.EventName)
	}
	if spec.PayloadType == nil {
		return compiledRoute{}, compiler.SourceError(declaration.Position,
			"@events: event %q has no payload type", declaration.EventName)
	}
	payloadType := spec.PayloadType
	for payloadType.Kind() == reflect.Pointer {
		payloadType = payloadType.Elem()
	}
	if payloadType.Kind() != reflect.Struct {
		return compiledRoute{}, compiler.SourceError(declaration.Position,
			"@events: event %q payload must be a struct", declaration.EventName)
	}

	compiled := compiledRoute{
		EventName:   spec.Name,
		Subject:     spec.Subject,
		BundleID:    spec.BundleID,
		PayloadType: reflect.PointerTo(payloadType),
	}
	if pipelineName, isPipeline := strings.CutPrefix(declaration.SinkName, "pipeline."); isPipeline {
		return materializePipelineRoute(req, declaration, spec, pipelineName, compiled)
	}
	if pipelines, ok := req.Meta["pipelines"].(map[string]string); ok {
		if _, declared := pipelines[declaration.SinkName]; declared {
			return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
				"@events: %q names a locally declared pipeline; did you mean pipeline.%s?", declaration.SinkName, declaration.SinkName)
		}
	}
	return materializeTypedSinkRoute(req, declaration, compiled)
}

func materializePipelineRoute(req compiler.MaterializeReq, declaration Route, spec compiler.MountedEventSpec, pipelineName string, compiled compiledRoute) (compiledRoute, error) {
	pipelines, _ := req.Meta["pipelines"].(map[string]string)
	if _, declared := pipelines[pipelineName]; !declared {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: pipeline handler %q must be declared in this source", declaration.SinkName)
	}
	canonicalName := pipelineName
	if !strings.Contains(canonicalName, ".") {
		canonicalName = "pipeline." + canonicalName
	}
	pipeline, exists := req.Resolver.Action(canonicalName)
	if !exists || pipeline == nil {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: unknown pipeline handler %q", declaration.SinkName)
	}
	requestSchema, hasRequest := requestSchemaSelector(pipeline)
	if !hasRequest {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: pipeline handler %q must declare a typed request schema", declaration.SinkName)
	}
	if strings.HasPrefix(strings.TrimPrefix(requestSchema, "!"), "[]") {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: pipeline handler %q request schema %q is array-shaped; event payload projection produces an object map",
			declaration.SinkName, requestSchema)
	}
	projector, err := compiler.NewExpressionProjector(spec.PayloadType)
	if err != nil {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: event %q payload cannot be projected to pipeline input: %v", declaration.EventName, err)
	}
	compiled.Pipeline = pipeline
	compiled.Projector = projector
	return compiled, nil
}

func materializeTypedSinkRoute(req compiler.MaterializeReq, declaration Route, compiled compiledRoute) (compiledRoute, error) {
	actionSink, exists := req.Resolver.Action(declaration.SinkName)
	if !exists || actionSink == nil {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
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
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: sink %q must implement the typed non-retaining event-sink contract", declaration.SinkName)
	}
	if sink.EventPayloadType() != compiled.PayloadType {
		return compiledRoute{}, compiler.SourceError(declaration.SinkPosition,
			"@events: sink %q accepts %s; event %q publishes %s",
			declaration.SinkName, sink.EventPayloadType(), declaration.EventName, compiled.PayloadType)
	}
	compiled.Sink = sink
	return compiled, nil
}

func requestSchemaSelector(handler action.AnyAction) (string, bool) {
	description := handler.Describe()
	if description == nil {
		return "", false
	}
	for _, tag := range description.Tags {
		if selector, ok := strings.CutPrefix(tag, "request-schema:"); ok {
			return selector, true
		}
	}
	return "", false
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
// may retain it. Direct local sinks are synchronous and must not retain it.
// Pipeline handlers run synchronously against a detached projected input map.
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
		if route.Pipeline != nil {
			binding, err := payloadExpressionBinding(payload, route.Projector)
			if err != nil {
				routeErr = err
			} else {
				// Invoke in the publisher's execution context so pipeline-body
				// actions retain their normal hooks and journal behavior; this
				// does not create a second runner root or event-delivery record.
				_, routeErr = action.InvokeAny(ctx, route.Pipeline, binding)
			}
		} else {
			routeErr = route.Sink.HandleEvent(ctx, payload)
		}
		routeErr = classifyEventError(routeErr, "event route handler failed", xerr.KindInternal)
	} else if publisher.delegate == nil && !routeHandled {
		return xerr.NotFound(fmt.Sprintf("no NFlow route or host publisher for event subject %q", subject))
	}

	var delegateErr error
	if publisher.delegate != nil {
		delegateErr = classifyEventError(
			publishToDelegate(ctx, publisher.delegate, bundleID, subject, payload, routeHandled || hasRoute),
			"host event publisher failed",
			xerr.KindUnavailable,
		)
	}
	if routeErr == nil {
		return delegateErr
	}
	if delegateErr == nil {
		return routeErr
	}
	return errors.Join(routeErr, delegateErr)
}

func classifyEventError(err error, message string, fallbackKind xerr.Kind) error {
	if err == nil {
		return nil
	}
	if appErr, ok := errors.AsType[*xerr.AppError](err); ok && appErr != nil {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return xerr.Timeout(message, context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return xerr.Canceled(message, context.Canceled)
	}
	if fallbackKind == xerr.KindUnavailable {
		return xerr.Unavailable(message)
	}
	return xerr.Internal(message)
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

// payloadExpressionBinding creates the detached NFlow input used by pipeline
// routes. Direct typed-sink routes do not pay this projection cost.
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
