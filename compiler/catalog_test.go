package compiler

import (
	"context"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestBuildCatalogIncludesMountedCanonicalActions(t *testing.T) {
	t.Parallel()

	probe := action.New("catalog.probe", func(_ context.Context, in any) (any, error) {
		return in, nil
	}).Description("catalog probe").Build()

	resolver, err := NewDynamicResolver(action.Library{
		Name:    "catalog_test",
		Actions: []action.AnyAction{probe},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolver() error = %v", err)
	}

	catalog := BuildCatalog(resolver, NewModifierTable(), NewDirectiveTable(), NewOperatorTable())
	got := make(map[string]bool, len(catalog.Atoms))
	for _, atom := range catalog.Atoms {
		got[atom.Name] = true
	}
	for _, name := range []string{"catalog.probe"} {
		if !got[name] {
			t.Errorf("catalog is missing mounted action name %q", name)
		}
	}
}

type catalogReadyPayload struct {
	BindAddress string `json:"bind_address"`
}

func TestBuildCatalogIncludesAliasMountedEventsAndPayloadFields(t *testing.T) {
	bundles := []Bundle{{
		ID:    "transport",
		Alias: "edge",
		Events: []EventSpec{{
			Name:        "thttp.events.listener_ready",
			Subject:     "events.listener_ready",
			Description: "listener is accepting connections",
			PayloadType: reflect.TypeFor[catalogReadyPayload](),
		}},
	}}
	mounted, err := BundleEventSpecs(bundles...)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatal(err)
	}
	catalog := BuildCatalog(resolver, NewModifierTable(), NewDirectiveTable(), NewOperatorTable(), mounted...)
	if len(catalog.Events) != 1 {
		t.Fatalf("catalog events = %#v", catalog.Events)
	}
	event := catalog.Events[0]
	if event.Name != "edge.events.listener_ready" {
		t.Fatalf("event name = %q, want alias-mounted source name", event.Name)
	}
	if event.Subject != "events.listener_ready" {
		t.Fatalf("event subject = %q, want canonical publisher subject", event.Subject)
	}
	if len(event.Payload) != 1 || event.Payload[0].Name != "bind_address" {
		t.Fatalf("event payload fields = %#v", event.Payload)
	}
	if mounted[0].Subject != "events.listener_ready" || mounted[0].BundleID != "transport" {
		t.Fatalf("mounted descriptor = %#v; alias must not change runtime identity", mounted[0])
	}
}

func TestBundleEventSpecsRejectsDuplicateCanonicalSubjects(t *testing.T) {
	payloadType := reflect.TypeFor[catalogReadyPayload]()
	_, err := BundleEventSpecs(
		Bundle{ID: "one", Events: []EventSpec{{Name: "one.ready", Subject: "events.ready", PayloadType: payloadType}}},
		Bundle{ID: "two", Events: []EventSpec{{Name: "two.ready", Subject: "events.ready", PayloadType: payloadType}}},
	)
	if err == nil {
		t.Fatal("duplicate publisher subject was accepted across bundles")
	}
}
