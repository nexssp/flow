# Action Input Validation

`@config:strict` + `:strict` / `:lenient` — runtime request validation
via `validation.Struct`.

## The problem

`->` is a data pipe, not a type pipe. When `A` returns `{name: "x"}` and
`B` expects `struct{Description string; Tier string}`, `Coerce` silently
zeroes the mismatched fields:

```
A: {name: "x", tier: "pro"}
         ↓ InvokeAny → Coerce[Req_B]
B: {Description: "", Tier: "pro"}   ← name vanished, no error
```

The compile-time edge check in `flow/pipeline_types.go` catches only
direct `A -> B`. Any projection `{ }` or `with @{ }` in between
disables the check, because the parser cannot see what flows through.

Runtime strict closes that gap.

## Four validation states

| `@config:strict` | `:strict` | `:lenient` | Validates? |
|:---:|:---:|:---:|:---:|
| – | – | – | ❌ |
| ✅ | – | – | ✅ |
| – | ✅ | – | ✅ |
| ✅ | – | ✅ | ❌ |
| ✅ | ✅ | ✅ | ❌ (`:lenient` wins) |

Zero `TypedPayload.ReqPayload()` (an `any` action) → validation never
runs, regardless of flags. There is no schema to check.

## How it works

1. **`buildAtom`** sees `strict == true` and the action is typed.
2. Calls `act.CloneWithHooks(validation.Hook())` **before**
   `wrapWithInjections` and `ApplyAll` turn it into `Dynamic` (which
   type-erases and loses `Req`).
3. Hook `OnBuild` checks whether `Req` carries `validate:` tags. If
   not → hook drops itself, the atom pays nothing.
4. Hook `Before` calls `validation.Struct(ctx, req)` on the coerced
   struct. Error → `xerr.ValidationDetails` → `KindValidation`.

## Three ways to enable

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
{ name: "Ada", email: "ada@nexss.dev" } -> create_user

# ❌ runtime: [Validation] validation failed
#    field "name": failed 'min' check (expected 3)
{ name: "Al", email: "ada@nexss.dev" } -> create_user

# ❌ runtime: "email" field missing
{ name: "Ada" } -> create_user
```

The error reaches the transport as `xerr.KindValidation` (HTTP 400).

## What strict does not do

- **Does not validate `Res`.** Only request, not response.
- **Does not run on `any` actions.** Without
  `TypedPayload.ReqPayload()` there is no schema.
- **Does not check at compile time.** A projection `{ }` is a runtime
  expression. The compile-time check (direct `A -> B`) is a separate
  mechanism in `pipeline_types.go`, always active, independent of
  strict.
- **Does not block `@{ }` injection.** Args are injected **before**
  the `Before` hook, so they join the input map in validation.
- **Does not check `@schema`.** `@schema` is a separate runtime guard
  for `map[string]any`, legacy from before tags. Both can coexist.

## Dependencies

`github.com/nexssp/validation` — the hook and `Struct` come from this
package. `flow/core/strict.go` only orchestrates (flag precedence,
installation order in `buildAtom`).

## Related

- `flow/docs/parameters.md` — three parameter channels (`@{ }`, input,
  `:mod`)
- `flow/pipeline_types.go` — compile-time edge check
- `nexssp/validation/action.go` — `ValidateAction`, `AutoValidate`,
  `validation.Hook()`
