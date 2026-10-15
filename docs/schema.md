# NFlow schemas

`@schema` declares a reusable object contract. Struct tags describe field names,
validation rules, and optional defaults:

```nflow
@schema PackInput {
  TreeDepth int    `json:"tree_depth" default:"1" validate:"gte=1"`
  Format    string `json:"format"     default:"markdown"`
}

@pipeline pack:schema=PackInput
  srcpack.pack
@end

{} -> pipeline.pack
```

The `:schema=PackInput` pipeline-header modifier applies the schema at every
invocation boundary, before the pipeline's first action. This works for a
programmatic call to the compiled `pipeline.pack` action as well as for a
pipeline invoked by a Flow or CLI listener. The NFlow schema tags remain the
single source of defaults and validation rules; callers do not need a separate
defaulting step.

A `default` tag supplies a value only when its JSON field is **absent**. An
explicit value—including `null`, `0`, or an empty slice—is preserved. Defaults
are applied before validation, so a defaulted field can satisfy
`validate:"required"`; all resulting values are still checked against the
field's type and validation rules. Malformed defaults are rejected when the
`@schema` declaration is compiled.

## Supported values

- `string`, booleans, integer types, and `float32`/`float64` use their natural
tag value, parsed according to the declared field type. For example:
`default:"false"`, `default:"12"`, and `default:"1.5"`.
- Slice, map, and struct fields use JSON in the tag value, such as
`default:"[\"a\",\"b\"]"` or
`default:"{\"enabled\":true}"`.
- `any` parses a valid JSON literal as JSON; otherwise the tag value remains a
string. For example, `default:"true"` is a boolean and `default:"fast"` is a
string.
- Slice fields accept Go slice values such as `[]string` and `[]any` without
converting or replacing the supplied value. The schema checks the container
kind; use validation tags such as `validate:"dive,required"` when element
constraints are needed.

Composite defaults are values, not deep merges: a supplied object or array
replaces the missing field as a whole. Other input fields are preserved.

## Effective values in Go

`schema.ApplyDefaults` returns a new map with defaults applied and does not
mutate the input. `schema.ApplyDefaultsAndValidate` does the same and also
validates the effective object:

```go
effective, err := schema.ApplyDefaultsAndValidate(ctx, packInputSchema, payload)
if err != nil {
    return nil, err
}
depth := effective["tree_depth"] // int(1) when the field was absent
```

`schema.Validate` applies defaults before checking the object. The materialized
`PackInput.validate` action returns the effective map to the next pipeline step,
so downstream actions can consume the values. `ToJSONSchema` also emits parsed
`default` values as JSON Schema annotations; use the Go APIs or the pipeline
boundary when actual value application is required.

`@const.load "settings.json" as settings :schema=Settings` likewise validates
the loaded object and makes any defaults available in the imported constants.

No Kernel API change is required: schema parsing, defaulting, validation, and
passing the effective object downstream are owned by Flow's schema and pipeline
extensions.
