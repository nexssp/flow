# Error handling: `? on_error` and `@on_error`

Flow has two error-recovery forms. They look similar but pass
different data to their recovery handlers — this is the single most
important thing to know when choosing between them.

## The two forms

| Form | Scope | Handler receives | Typical use |
|---|---|---|---|
| `expr ? on_error { ... }` | one expression | an *error environment* map | "turn this failure into a value" |
| `@on_error { ... }` | whole pipeline | the *original request* | "route this failure to a named recovery pipeline" |

The error environment contains:

```
{
  input:   <the request that failed>,
  error:   { kind, message, original, cause, details },
  xerr:    { KindValidation: "Validation", KindTimeout: "Timeout", … },
  __root__: <alias of input>,
}
```

The directive form is a pipeline wrapper, so its handler is invoked
with the same request the failing stage saw. `error.info` reads the
recovered error from the execution context instead.

## Short forms

When there is only one recovery target and no kind discrimination, use
the short form. It is exactly equivalent to a block with a single
`else` arm.

```
runtime.fail @{ kind: "Internal" } ? on_error error.passthrough
```

```
@on_error error.passthrough
runtime.fail @{ kind: "Internal" }
```

Anything after `? on_error` up to the next operator is the target. If
you want a pipeline as the recovery target, wrap it in parentheses:

```
runtime.fail ? on_error (recover.a -> recover.b)
```

## Standard recovery actions

The `on_error` extension ships four actions designed for these blocks:

| Action | Effect | Available in |
|---|---|---|
| `error.info` | Returns `{ kind, message }` from the recovered error | `@on_error` (reads ctx) |
| `error.reraise` | Re-propagates the original error so an enclosing `\|\|` can catch it | both |
| `error.passthrough` | Returns the original request, discarding the error envelope | both |
| (any registered action) | Whatever it does | both |

`error.reraise` is what you reach for when you want to *observe*
without *handling*:

```
@on_error error.reraise
log.warn @{ message: "pipeline failed" }
... the rest of the pipeline ...
```

Actually re-raising defeats the purpose of the log — the pipeline still
fails. The correct pattern is a small pipeline that logs and then
re-raises:

```
@pipeline observe_and_fail
  log.warn @{ message: "pipeline failed" }
  -> error.reraise
@end
```

## Kind-discriminated recovery

When different error kinds need different recoveries, use the block
form:

```
@on_error {
  when error.kind == xerr.KindTimeout  -> pipeline.on_timeout
  when error.kind == xerr.KindNotFound -> pipeline.on_missing
  else                                  -> error.passthrough
}
```

Or on a single expression:

```
TicketTriage.validate ? on_error {
  xerr.KindValidation -> { status: "rejected", error: .error.message },
  xerr.KindUnavailable -> { status: "deferred" },
  else -> error.reraise,
}
```

Case bodies use the full expression language. The `else` clause is
optional; when absent and no kind matches, the original error
propagates unchanged.

## Invocation conventions (the gotcha)

The two forms deliver the handler different inputs. If your recovery
action reads `.customer_id` directly, it will work in `@on_error` and
return `nil` in `? on_error` — because in the latter it should read
`.input.customer_id`.

To write a recovery action that works in both:

```go
func recoverAction(ctx context.Context, req any) (any, error) {
    customerID := ""
    if m, ok := req.(map[string]any); ok {
        if inner, ok := m["input"].(map[string]any); ok {
            customerID, _ = inner["customer_id"].(string)
        } else {
            customerID, _ = m["customer_id"].(string)
        }
    }
    // ...
}
```

Or use `error.passthrough` as the entry point and chain from there.

## Terminal cancellation

Neither form runs its recovery handler when the caller context is
already cancelled or a standard `context.Canceled` is returned. This
is enforced in `terminalRecoveryError`. Typed `xerr.Canceled` (without
standard context cancellation) remains a matchable Kernel kind.

## Interaction with `||`

`||` is a separate operator (Kernel `FirstSuccessAny`). It catches any
error and runs its right arm. `? on_error` is a postfix on one
expression. Both can coexist:

```
maybe_fails ? on_error { xerr.KindTimeout -> recover.timeout }
  || const @{ value: "last_resort" }
```

Precedence: `?` binds tighter than `||`, so the guard applies to
`maybe_fails` and the `||` sees the guard's result. If the guard's arm
succeeds, `||` does not fire. If the guard's arm *errors* (e.g. via
`error.reraise`), `||` catches it.
