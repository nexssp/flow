# Reusable text templates

Flow's text renderer lives in the `template` extension, alongside its Flow action:

```go
import flowtemplate "github.com/nexssp/flow/extensions/template"

result, err := flowtemplate.Render(
    "Hello {{.name}} ({{.profile.city}})",
    map[string]any{
        "name":    "srcpack",
        "profile": map[string]any{"city": "Warsaw"},
    },
)
```

`Render(source, variables)` uses Go's standard `text/template` syntax. Values
stay data: inserted text is not parsed again as template source, and rendering
never invokes a shell. The result is plain text, not HTML-escaped output; use an
HTML-specific renderer when HTML-context escaping is required.

A referenced missing map key (for example, `.name` when `name` is absent) is an
execution error. Invalid syntax and unknown template functions are parse errors.
`errors.Is(err, flowtemplate.ErrExecute)` and
`errors.Is(err, flowtemplate.ErrParse)` distinguish these categories. Extra
keys are accepted and ignored if unused, so callers can reuse larger variable
maps. On failure, the result is empty; partial output is not returned. A nil
variable map is treated as empty.

The same extension exposes the `template.render` Flow action with
`{template, variables}` input. It is not included in the stock CLI's built-in
bundle set; a Flow distribution can include the extension or a workflow can
require it explicitly.
