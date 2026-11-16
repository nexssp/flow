# Runtime action-request validation

Flow's `@config:strict=true` and atom modifiers `:strict` / `:lenient` opt
typed action requests into the validation hook from
[`github.com/nexssp/validation`](https://pkg.go.dev/github.com/nexssp/validation).
This is distinct from named `@schema` pipeline-boundary validation; see
[`schema.md`](schema.md).

## The problem

Pipeline edges pass values through the action coercion boundary. For a Flow
projection immediately followed by a typed action, the compiler compares the
inferred JSON shapes of explicit fields, including nested objects and arrays;
a definite shape mismatch is rejected before execution. Numeric-to-numeric
conversions and dynamic/interface values remain subject to runtime coercion,
which may reject particular values. A runtime map may also contain fields that
do not map to a typed request field, so use validation tags when the resulting
request needs required-field or value checks:

```
A: {name: "x", tier: "pro"}
         ↓ runtime coercion to B's typed request
B: fields populated according to its JSON field names; validation tags
  can reject absent or invalid required values
```

The compiler's typed-edge and expression analysis is in
`compiler/analyze.go` and `compiler/typed_analysis.go`. It propagates declared
action outputs and inferable projection shapes. Ordinary typed edges use
conservative contract compatibility; projection edges reject definite JSON
shape mismatches while leaving value-dependent numeric conversions and
dynamic values to runtime coercion. Runtime strict validation is useful for
typed action request structs whose `validate:` tags should be enforced after
request coercion.

## Four validation states

| `@config:strict=true` | `:strict` | `:lenient` | Validates? |
|:---:|:---:|:---:|:---:|
| – | – | – | ❌ |
| ✅ | – | – | ✅ |
| – | ✅ | – | ✅ |
| ✅ | – | ✅ | ❌ |
| ✅ | ✅ | ✅ | ❌ (`:lenient` wins) |

No typed, struct-shaped request with validation rules (or `Validatable`)
means the validation hook has nothing to check.

## How it works

1. Atom construction reads global strict configuration and the atom's
   `:strict` / `:lenient` markers.
2. For a typed action request, Flow clones the action with
   `validation.Hook()` before finalizing the atom for execution.
3. The hook's `OnBuild` predicate keeps itself only for request types with
   validation tags or the `Validatable` contract.
4. Before the action runs, the hook validates its coerced request with
   `validation.Struct`; failures are returned as structured
   `xerr.Validation` errors.

## Three ways to enable

These snippets assume the named producer/consumer actions are registered by
the embedding host or an explicitly loaded bundle.

**Per file:**
```nflow
@config:strict=true

producer -> { name: .name } -> consumer
```

**Per atom:**
```nflow
producer -> { name: .name } -> consumer:strict
```

**Opt-out per atom (when global strict is on):**
```nflow
@config:strict=true
producer -> { internal_thing: .x } -> helper:lenient
```

## Examples

Action with tags:
```go
type CreateUserReq struct {
    Name  string `json:"name"  validate:"required,min=3"`
    Email string `json:"email" validate:"required,email"`
}
```

Flow:
```nflow
@config:strict=true

# ✅ passes
{ name: "Maksymilian", email: "julia@nexss.dev" } -> create_user

# ❌ runtime: [Validation] validation failed
#    field "name": failed 'min' check (expected 3)
{ name: "Al", email: "julia@nexss.dev" } -> create_user

# ❌ runtime: "email" field missing
{ name: "Maksymilian" } -> create_user
```

The runtime error is classified as `xerr.KindValidation`. A transport may
map that kind to an HTTP response; the CLI does not turn every validation
failure into an HTTP request.

## What strict does not do

- **Does not validate responses.** It validates the action request only.
- **Does not validate arbitrary `any` or map requests.** The validation
  package's hook is enabled for eligible typed request structures.
- **Does not perform compile-time payload validation itself.** The separate
  typed analysis checks statically known action edges and inferable projection
  results; it does not prove future or dynamic values or enforce request
  validation tags.
- **Does not prohibit action argument binding.** Validation applies to the
  typed request the action receives after the atom's inputs are assembled.
- **Does not replace `@schema`.** Named schemas validate/default pipeline
  payloads at a pipeline boundary or through `NAME.validate`; the two
  mechanisms may be used together.

## Dependencies

`github.com/nexssp/validation` provides `Hook`, `Struct`, and `Validatable`.
[`compiler/strict.go`](../compiler/strict.go) reads global strict config;
[`compiler/ast_build.go`](../compiler/ast_build.go) attaches the hook while
building an atom.

## Related

- [Language reference](language-reference.md) — argument, expression, and
  validation boundaries
- [`compiler/analyze.go`](../compiler/analyze.go) — typed-edge analysis
- [Schema documentation](schema.md) — named schemas and pipeline validation
