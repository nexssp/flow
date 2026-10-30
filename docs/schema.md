# NFlow schemas

`@schema` declares a reusable object contract. Struct tags describe field names,
validation rules, and optional defaults:

```nflow
@schema PackInput {
  TreeDepth int    `json:"tree_depth" default:"1" validate:"gte=1"`
  Format    string `json:"format"     default:"markdown"`
}

@pipeline pack
  :request-schema=PackInput
  srcpack.pack
@end

{} -> pipeline.pack
```

The `:request-schema=PackInput` pipeline-header modifier applies the schema at
every request boundary, before the pipeline's first action. This works for a
programmatic call to the compiled `pipeline.pack` action as well as for a
pipeline invoked by a Flow or CLI listener. The NFlow schema tags remain the
single source of defaults and validation rules; callers do not need a separate
defaulting step. A `:response-schema=PackOutput` modifier validates the result
after the pipeline body without applying defaults. Response schemas must not
declare any `default` tag; the compiler reports a clear error before execution
if one is present, even when its value is empty.

Response checks accept typed Go structs as well as maps. The boundary validates
an inspection view produced by `schema.NormalizeObject` and returns the original
Go value unchanged, so a typed result remains typed for callers and later
pipeline composition. The helper follows `encoding/json`: exported fields and
their `json` names are used, `json:"-"` and unexported fields are ignored,
embedded fields follow Go JSON promotion rules, nested structs become nested
maps, nil pointer fields become JSON `null` unless omitted by a tag, and
`omitempty` fields may be absent. A nil top-level value or non-object JSON value
is rejected. Strict response schemas check every JSON-visible top-level field;
permissive schemas allow undeclared fields without projecting them away.

By default, fields not listed in a request or response schema are allowed and
preserved. Prefix a schema name with `!` to make only that boundary reject
unknown fields, for example `:request-schema=!PackInput` or
`:response-schema=!PackOutput`. Request and response strictness are independent.
The former pipeline-header alias `:schema=PackInput` is no longer supported;
use `:request-schema=PackInput` instead.

For shorter headers, `:in-schema=PackInput` is an additive alias for
`:request-schema=PackInput`, and `:out-schema=PackOutput` aliases
`:response-schema=PackOutput`. The long forms remain fully supported and are
not deprecated. Do not specify both spellings for the same boundary in one
pipeline header; Flow rejects the duplicate rather than guessing which one wins.

Schemas from an explicitly included module are reusable by their qualified
name; they do not become unqualified declarations in the importing module. If
`memory_types.nflow` declares `@schema ChatTurn`, a caller can write:

```nflow
@include "./memory_types.nflow" AS memory
@pipeline memory_search
  :in-schema=!memory.ChatTurn
  runtime.noop
@end
```

The same qualified name is available to `:request-schema`, `:response-schema`,
`:out-schema`, and `schema.validate` in that importing source. A schema declared
locally with the same name remains distinct from `memory.ChatTurn`.

A `default` tag supplies a value only when its JSON field is **absent**. An
explicit value—including `null`, `0`, or an empty slice—is preserved. Defaults
are applied before validation, so a defaulted field can satisfy
`validate:"required"`; all resulting values are still checked against the
field's type and validation rules. Malformed defaults are rejected when the
`@schema` declaration is compiled.

Schema `default` and `validate` tag values can reference previously declared
symbols. This lets the same values drive both application logic and typed
validation:

```nflow
@symbols memory { search, put, chat_turn }
@schema MemoryRequest {
  Operation string `json:"operation" default:"memory.search" validate:"required,oneof=memory.search memory.put memory.chat_turn"`
}
```

Bare symbol declarations use their names as string values, so this default
becomes `"search"` and the `oneof` rule accepts only the three declared
operations. Resolved defaults are then parsed according to the field's declared
type, so a string-valued symbol such as `"201"` can provide an integer default.
Declare the symbol namespace before the schema; an unknown member inside a
declared namespace is a compile-time error. See [Symbols](symbols.md) for scope
and declaration forms.

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
