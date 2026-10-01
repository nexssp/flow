package template

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

func TestBundle_WiresTemplateAction(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	if bundle.ID != ID {
		t.Fatalf("Bundle().ID = %q, want %q", bundle.ID, ID)
	}
	if err := core.ValidateBundle(bundle); err != nil {
		t.Fatalf("ValidateBundle() error = %v", err)
	}
	if len(bundle.Libraries) != 1 || bundle.Libraries[0].Name != ID {
		t.Fatalf("Bundle().Libraries = %#v, want one %q library", bundle.Libraries, ID)
	}
	if got := bundle.Libraries[0].Actions; len(got) != 1 || got[0].Describe().Name != "template.render" {
		t.Fatalf("template library actions = %#v, want template.render", got)
	}
}

func TestRenderAction_RendersAndReportsMissingVariables(t *testing.T) {
	t.Parallel()

	var act action.AnyAction = RenderAction
	got, err := act.DoAny(context.Background(), RenderRequest{
		Template:  "Hello {{.name}}",
		Variables: map[string]any{"name": "Flow"},
	})
	if err != nil {
		t.Fatalf("RenderAction.DoAny() error = %v", err)
	}
	if got != "Hello Flow" {
		t.Fatalf("RenderAction.DoAny() = %#v, want %q", got, "Hello Flow")
	}

	_, err = act.DoAny(context.Background(), RenderRequest{Template: "{{.missing}}"})
	if err == nil {
		t.Fatal("RenderAction.DoAny() error = nil, want missing-variable error")
	}
	if !strings.Contains(err.Error(), "template execution error") {
		t.Fatalf("RenderAction.DoAny() error = %v, want renderer context", err)
	}
}
