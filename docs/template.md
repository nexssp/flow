# Reusable text templates

Flow includes a package-agnostic renderer at `github.com/nexssp/flow/template`:

```go
import flowtemplate "github.com/nexssp/flow/template"

result, err := flowtemplate.Render(
    "Hello {{.name}} ({{.profile.city}})",
    map[string]any{
        "name":    "srcpack",
        "profile": map[string]any{"city": "Warsaw"},
    },
)
```

`Render(source, variables)` uses Go's standard `text/template` syntax and has no
additional dependencies. Values stay data: text inserted from a variable is not
parsed again as template source, and the function never invokes a shell. The
result is plain text, not HTML-escaped output; use an HTML-specific renderer
when HTML context escaping is required.

A referenced missing map key (for example, `.name` when `name` is absent) is an
execution error. Invalid syntax and unknown template functions are parse errors.
`errors.Is(err, flowtemplate.ErrExecute)` and
`errors.Is(err, flowtemplate.ErrParse)` distinguish these categories. Extra
keys are intentionally accepted and ignored if the template does not use them,
so callers can reuse larger variable maps. On failure, the result is empty;
partial output is not returned. A nil variable map is treated as empty.

An optional Flow adapter is available from `github.com/nexssp/flow/extensions/template`.
It exposes `template.render` with `{template, variables}` input. The adapter is
not added to the stock CLI's built-in import list: a Flow distribution must
import this bundle or require it as an extension. The standalone Go API does
not depend on Flow's bundle or action packages.
