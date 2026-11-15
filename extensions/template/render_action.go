package template

import (
	"context"
	"errors"
	"io"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"

	"github.com/nexssp/flow/compiler/contracts"
)

// RenderRequest is the input shape for the template.render Flow action.
type RenderRequest struct {
	// Template is an inline string or an embedded xfile.Ref. Set exactly one
	// of Template and TemplateBytes.
	Template any `json:"template,omitempty" cli:"template"`
	// TemplateBytes is the source content, such as fs.read's .Content bytes.
	TemplateBytes []byte         `json:"template_bytes,omitempty" cli:"template_bytes"`
	Variables     map[string]any `json:"variables"                cli:"variables"`
}

const maxTemplateSourceBytes int64 = 16 << 20

// RenderAction exposes the package-agnostic renderer to Flow workflows.
var RenderAction = action.New("template.render", func(ctx context.Context, req RenderRequest) (string, error) {
	source, err := templateSource(ctx, req.Template, req.TemplateBytes)
	if err != nil {
		return "", err
	}
	result, err := Render(source, req.Variables)
	if err != nil {
		return "", xerr.BadRequest(err.Error())
	}
	return result, nil
}).Description("Render a text template with supplied variables").
	Example(RenderRequest{
		Template:  "Hello {{.name}}",
		Variables: map[string]any{"name": "Flow"},
	}).Build()

func templateSource(ctx context.Context, source any, sourceBytes []byte) (string, error) {
	hasSource := source != nil
	hasSourceBytes := sourceBytes != nil
	if hasSource == hasSourceBytes {
		return "", xerr.Validation("template.render: provide exactly one of template or template_bytes")
	}
	if hasSourceBytes {
		if int64(len(sourceBytes)) > maxTemplateSourceBytes {
			return "", xerr.BadRequest("template.render: template source exceeds the 16 MiB limit")
		}
		return string(sourceBytes), nil
	}

	switch value := source.(type) {
	case string:
		if int64(len(value)) > maxTemplateSourceBytes {
			return "", xerr.BadRequest("template.render: template source exceeds the 16 MiB limit")
		}
		return value, nil
	case xfile.Ref:
		return readTemplateRef(ctx, value)
	default:
		return "", xerr.Validation("template.render: template must be a string or embedded file reference")
	}
}

func readTemplateRef(ctx context.Context, ref xfile.Ref) (string, error) {
	if ctx == nil {
		return "", xerr.BadRequest("template.render: context is nil")
	}
	resolver := contracts.FileResolverFromContext(ctx)
	if resolver == nil {
		return "", xerr.Unavailable("template.render: file resolver is not configured")
	}
	reader, _, err := resolver.Open(ctx, ref)
	if err != nil {
		return "", err
	}
	if reader == nil {
		return "", xerr.Internal("template.render: file resolver returned a nil reader")
	}

	data, readErr := io.ReadAll(io.LimitReader(reader, maxTemplateSourceBytes+1))
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", xerr.Canceled("template.render: reading template was canceled", ctxErr)
		}
		return "", xerr.Unavailable("template.render: failed to read template asset", err)
	}
	if err := ctx.Err(); err != nil {
		return "", xerr.Canceled("template.render: reading template was canceled", err)
	}
	if int64(len(data)) > maxTemplateSourceBytes {
		return "", xerr.BadRequest("template.render: template source exceeds the 16 MiB limit")
	}
	return string(data), nil
}
