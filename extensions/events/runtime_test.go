package events

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

type projectionNested struct {
	Label string `json:"label"`
}

type projectionCustom string

func (value projectionCustom) MarshalJSON() ([]byte, error) {
	return json.Marshal("custom:" + string(value))
}

type projectionCustomStruct struct {
	Value int `json:"value"`
}

func (value projectionCustomStruct) MarshalJSON() ([]byte, error) {
	return json.Marshal("custom struct codec")
}

type projectionPayload struct {
	Count    int                    `json:"count"`
	Nested   projectionNested       `json:"nested"`
	Optional string                 `json:"optional,omitempty"`
	Custom   projectionCustom       `json:"custom"`
	Struct   projectionCustomStruct `json:"struct"`
	Maybe    *projectionNested      `json:"maybe"`
}

func TestPayloadExpressionBindingPreservesDeclaredValues(t *testing.T) {
	projector, err := compiler.NewExpressionProjector(reflect.TypeFor[projectionPayload]())
	if err != nil {
		t.Fatal(err)
	}
	payload := projectionPayload{
		Count:  7,
		Nested: projectionNested{Label: "ready"},
		Custom: projectionCustom("value"),
		Struct: projectionCustomStruct{Value: 9},
	}
	binding, err := payloadExpressionBinding(payload, projector)
	if err != nil {
		t.Fatalf("payloadExpressionBinding: %v", err)
	}
	if got, ok := binding["count"].(int); !ok || got != 7 {
		t.Fatalf("count = %#v (%T), want int(7)", binding["count"], binding["count"])
	}
	nested, ok := binding["nested"].(map[string]any)
	if !ok || nested["label"] != "ready" {
		t.Fatalf("nested = %#v, want JSON-tagged label map", binding["nested"])
	}
	if got, optionalOK := binding["optional"].(string); !optionalOK || got != "" {
		t.Fatalf("optional = %#v (%T), want present zero string", binding["optional"], binding["optional"])
	}
	if got, customOK := binding["custom"].(projectionCustom); !customOK || got != "value" {
		t.Fatalf("custom = %#v (%T), want original named Go string", binding["custom"], binding["custom"])
	}
	customStruct, ok := binding["struct"].(map[string]any)
	if !ok || customStruct["value"] != 9 {
		t.Fatalf("struct = %#v, want declared fields despite custom marshaler", binding["struct"])
	}
	if got, exists := binding["maybe"]; !exists || got != nil {
		t.Fatalf("maybe = %#v, want explicit null for nil pointer", got)
	}
}

func TestPayloadExpressionBindingRejectsMismatchedAndNilPayloads(t *testing.T) {
	projector, err := compiler.NewExpressionProjector(reflect.TypeFor[projectionPayload]())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payloadExpressionBinding(struct{ Count int }{}, projector); err == nil || xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("mismatched payload error = %v, want Validation", err)
	}
	var payload *projectionPayload
	if _, err := payloadExpressionBinding(payload, projector); err == nil || xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("nil payload error = %v, want Validation", err)
	}
	if _, err := payloadExpressionBinding(payload, nil); err == nil || xerr.KindFrom(err) != xerr.KindInternal {
		t.Fatalf("nil projector error = %v, want Internal", err)
	}
}

type projectionCountingPublisher struct {
	calls int
}

func (publisher *projectionCountingPublisher) PublishEvent(context.Context, string, any) error {
	publisher.calls++
	return nil
}

func TestRoutedPublisherDoesNotDelegateMismatchedPayload(t *testing.T) {
	sink := NewNonRetainingEventSink[projectionPayload]("test.sink", func(context.Context, *projectionPayload) error {
		return nil
	})
	delegate := &projectionCountingPublisher{}
	publisher := routedPublisher{
		routes: &compiledRouteSet{bySubject: map[string]compiledRoute{
			"events.projection": {
				EventName:   "test.events.projection",
				Subject:     "events.projection",
				BundleID:    "test",
				PayloadType: reflect.TypeFor[*projectionPayload](),
				Sink:        sink,
			},
		}},
		delegate: delegate,
	}
	ctx := context.Background()
	if err := publisher.PublishEventFromBundle(ctx, "test", "events.projection", &struct{ Count int }{Count: 7}); err == nil {
		t.Fatal("expected mismatched payload error")
	}
	if delegate.calls != 0 {
		t.Fatalf("delegate calls = %d, want 0 for rejected payload", delegate.calls)
	}
}

func TestPublishFromBundleClassifiesRuntimeErrors(t *testing.T) {
	payload := &projectionPayload{}
	tests := []struct {
		name    string
		ctx     context.Context
		payload *projectionPayload
		want    xerr.Kind
	}{
		{name: "nil payload", ctx: context.Background(), want: xerr.KindBadRequest},
		{name: "nil context", payload: payload, want: xerr.KindBadRequest},
		{name: "missing publisher", ctx: context.Background(), payload: payload, want: xerr.KindUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := PublishFromBundle(test.ctx, "test", "events.projection", test.payload)
			if got := xerr.KindFrom(err); got != test.want {
				t.Fatalf("error kind = %q, want %q: %v", got, test.want, err)
			}
		})
	}
}

func TestRoutedPublisherClassifiesRuntimeErrors(t *testing.T) {
	route := compiledRoute{
		EventName:   "test.events.projection",
		Subject:     "events.projection",
		BundleID:    "test",
		PayloadType: reflect.TypeFor[*projectionPayload](),
	}
	publisher := routedPublisher{routes: &compiledRouteSet{bySubject: map[string]compiledRoute{
		route.Subject: route,
	}}}
	payload := &projectionPayload{}
	tests := []struct {
		name      string
		publisher routedPublisher
		ctx       context.Context
		bundleID  string
		subject   string
		payload   any
		want      xerr.Kind
	}{
		{name: "nil context", publisher: publisher, subject: route.Subject, payload: payload, bundleID: "test", want: xerr.KindBadRequest},
		{name: "unauthorized producer", publisher: publisher, ctx: context.Background(), subject: route.Subject, payload: payload, bundleID: "attacker", want: xerr.KindForbidden},
		{name: "wrong payload type", publisher: publisher, ctx: context.Background(), subject: route.Subject, payload: &struct{ Count int }{}, bundleID: "test", want: xerr.KindValidation},
		{name: "missing route and publisher", publisher: routedPublisher{routes: &compiledRouteSet{bySubject: map[string]compiledRoute{}}}, ctx: context.Background(), subject: route.Subject, payload: payload, bundleID: "test", want: xerr.KindNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.publisher.PublishEventFromBundle(test.ctx, test.bundleID, test.subject, test.payload)
			if got := xerr.KindFrom(err); got != test.want {
				t.Fatalf("error kind = %q, want %q: %v", got, test.want, err)
			}
		})
	}
}

func TestNonRetainingEventSinkClassifiesPayloadErrors(t *testing.T) {
	sink := NewNonRetainingEventSink[projectionPayload]("test.sink", func(context.Context, *projectionPayload) error {
		return nil
	})
	if err := sink.HandleEvent(context.Background(), &struct{ Count int }{}); xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("mismatched payload error = %v, want Validation", err)
	}
	if err := sink.HandleEvent(context.Background(), (*projectionPayload)(nil)); xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("nil payload error = %v, want Validation", err)
	}
}

func TestMaterializeRoutesClassifiesMissingResolverAsInternal(t *testing.T) {
	source := "@events {\n  observe sample.events.ready -> sample.sink\n}\n"
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), source, "missing-resolver.nflow")
	if err != nil {
		t.Fatalf("preprocess route: %v", err)
	}
	if err := MaterializeRoutes(compiler.MaterializeReq{Meta: meta}); xerr.KindFrom(err) != xerr.KindInternal {
		t.Fatalf("missing resolver error = %v, want Internal", err)
	}
}
