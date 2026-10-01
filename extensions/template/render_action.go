package template

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	flowtemplate "github.com/nexssp/flow/template"
)

// RenderRequest is the input shape for the template.render Flow action.
type RenderRequest struct {
	Template  string         `json:"template" cli:"template"`
	Variables map[string]any `json:"variables" cli:"variables"`
}

// RenderAction exposes the package-agnostic renderer to Flow workflows.
var RenderAction = action.New("template.render", func(_ context.Context, req RenderRequest) (string, error) {
	result, err := flowtemplate.Render(req.Template, req.Variables)
	if err != nil {
		return "", xerr.BadRequest(err.Error())
	}
	return result, nil
}).Description("Render a text template with supplied variables").
	Example(RenderRequest{
		Template:  "Hello {{.name}}",
		Variables: map[string]any{"name": "Flow"},
	}).Build()
