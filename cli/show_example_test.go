package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

type showExampleRequest struct {
	AccountID string `json:"account_id"`
}

func TestShowAtomPrintsTypedKernelExample(t *testing.T) {
	act := action.New("example.echo", func(_ context.Context, req showExampleRequest) (string, error) {
		return req.AccountID, nil
	}).Example(showExampleRequest{AccountID: "acct-demo"}).Build()

	resolver, err := compiler.NewDynamicResolver(action.Library{
		Name:    "show_example_test",
		Actions: []action.AnyAction{act},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolver() error = %v", err)
	}

	catalog := compiler.BuildCatalog(
		resolver,
		compiler.NewModifierTable(),
		compiler.NewDirectiveTable(),
		compiler.NewOperatorTable(),
	)
	stdout, _ := captureIO(t, func() {
		if !showAtom(catalog.Atoms, "example.echo", false) {
			t.Error("showAtom() did not find example.echo")
		}
	})
	for _, want := range []string{"EXAMPLE", `"account_id": "acct-demo"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show output missing %q:\n%s", want, stdout)
		}
	}
}

func TestShowEventPrintsCanonicalSubject(t *testing.T) {
	event := compiler.EventCatalogSpec{
		Name:    "edge.events.listener_ready",
		Subject: "events.listener_ready",
		Payload: []compiler.FieldSpec{{Name: "bind_address", Type: "string"}},
	}
	stdout, _ := captureIO(t, func() {
		if !showEvent([]compiler.EventCatalogSpec{event}, event.Name, false) {
			t.Error("showEvent() did not find the mounted event")
		}
	})
	for _, want := range []string{"edge.events.listener_ready", "Subject: events.listener_ready", "bind_address"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show output missing %q:\n%s", want, stdout)
		}
	}
}

func TestRenderListIncludesMountedEvents(t *testing.T) {
	catalog := compiler.Catalog{Events: []compiler.EventCatalogSpec{{
		Name:        "edge.events.listener_ready",
		Subject:     "events.listener_ready",
		Description: "HTTP listener is ready",
	}}}
	stdout, _ := captureIO(t, func() {
		renderList(catalog, "listener_ready", false)
	})
	for _, want := range []string{"EVENTS", "edge.events.listener_ready", "HTTP listener is ready"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("list output missing %q:\n%s", want, stdout)
		}
	}
}
