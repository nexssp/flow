package template

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

func TestBundle_WiresTemplateAction(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	if bundle.ID != ID {
		t.Fatalf("Bundle().ID = %q, want %q", bundle.ID, ID)
	}
	if err := compiler.ValidateBundle(bundle); err != nil {
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

func TestRenderAction_RendersUTF8TemplateBytes(t *testing.T) {
	t.Parallel()

	var act action.AnyAction = RenderAction
	got, err := act.DoAny(context.Background(), RenderRequest{
		TemplateBytes: []byte("Welcome {{.name}} — Grüß Gott, 世界 🌍"),
		Variables:     map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("RenderAction.DoAny() error = %v", err)
	}
	if got != "Welcome Ada — Grüß Gott, 世界 🌍" {
		t.Fatalf("RenderAction.DoAny() = %#v, want UTF-8 output", got)
	}
}

func TestRenderAction_RequiresExactlyOneTemplateSource(t *testing.T) {
	t.Parallel()

	var act action.AnyAction = RenderAction
	for _, tc := range []struct {
		name string
		req  RenderRequest
	}{
		{name: "missing"},
		{name: "both", req: RenderRequest{Template: "inline", TemplateBytes: []byte("file")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := act.DoAny(context.Background(), tc.req)
			if xerr.KindFrom(err) != xerr.KindValidation {
				t.Fatalf("RenderAction.DoAny() error = %v, want Validation", err)
			}
		})
	}
}

func TestRenderAction_RejectsOversizedTemplateBytes(t *testing.T) {
	t.Parallel()

	var act action.AnyAction = RenderAction
	_, err := act.DoAny(context.Background(), RenderRequest{
		TemplateBytes: make([]byte, maxTemplateSourceBytes+1),
	})
	if xerr.KindFrom(err) != xerr.KindBadRequest {
		t.Fatalf("RenderAction.DoAny() error = %v, want BadRequest", err)
	}
}

func TestRenderAction_RejectsUnsupportedTemplateSource(t *testing.T) {
	t.Parallel()

	var act action.AnyAction = RenderAction
	_, err := act.DoAny(context.Background(), RenderRequest{Template: 42})
	if xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("RenderAction.DoAny() error = %v, want Validation", err)
	}
}
