# NFlow schemas

`@schema` declares a reusable object contract. Struct tags describe field names,
validation rules, and optional defaults:

```nflow
@schema PackInput {
  TreeDepth int    `json:"tree_depth" default:"1" validate:"gte=1"`
  Format    string `json:"format"     default:"markdown"`
}

{} -> PackInput.validate
```

A `default` tag supplies a value only when its JSON field is **absent**. An
explicit value—including `null`—is preserved. Defaults are applied before
validation, so a defaulted field can satisfy `validate:"required"`; all
resulting values are still checked against the field's type and validation
rules. Malformed defaults are rejected when the `@schema` declaration is
compiled.

## Supported values

- `string`, booleans, integer types, and `float32`/`float64` use their natural
  tag value, parsed according to the declared field type. For example:
  `default:"false"`, `default:"12"`, and `default:"1.5"`.
- Slice, map, and struct fields use JSON in the tag value, such as
  `default:"[\"a\",\"b\"]"` or
  `default:"{\"enabled\":true}"`.
- `any` parses a valid JSON literal as JSON; otherwise the tag value remains a
  string. For example, `default:"true"` is a boolean and
  `default:"fast"` is a string.

Composite defaults are values, not deep merges: a supplied object or array
replaces the missing field as a whole. Other input fields are preserved.

## Effective values in Go

`schema.ApplyDefaults` is the public API for runtime or CLI consumers that need
the resulting values. It returns a new map and does not mutate the input:

```go
values, err := schema.ApplyDefaults(packInputSchema, map[string]any{})
if err != nil {
    return err
}
depth := values["tree_depth"] // int(1)
```

`schema.Validate` applies defaults before checking the object. The materialized
`PackInput.validate` action returns the effective map to the next pipeline step,
so downstream actions can consume the values. `ToJSONSchema` also emits parsed
`default` values as JSON Schema annotations; use `ApplyDefaults` when actual
value application is required.

`@const.load "settings.json" as settings :schema=Settings` likewise validates
the loaded object and makes any defaults available in the imported constants.

No Kernel extension is required: parsing, defaulting, validation, and passing
the effective object downstream are owned by Flow's `extensions/schema`
package.
