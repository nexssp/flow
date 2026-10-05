# Flow v0.15.0 design: `? on_error`

This note fixes the grammar and runtime contract for the owner-approved v0.15.0 expression-level error guard before parser changes begin.

## Grammar

```ebnf
error_guard  = expression, "?", "on_error", "{", case_list, "}" ;
case_list    = case, { ",", case } ;
case         = kind_symbol, "->", expression
             | "else", "->", expression ;
kind_symbol  = "xerr.KindBadRequest" | "xerr.KindUnauthorized"
             | "xerr.KindForbidden" | "xerr.KindNotFound"
             | "xerr.KindConflict" | "xerr.KindValidation"
             | "xerr.KindTooManyRequests" | "xerr.KindTimeout"
             | "xerr.KindUnavailable" | "xerr.KindInternal"
             | "xerr.KindMethodNotAllowed" | "xerr.KindRateLimit"
             | "xerr.KindCanceled" | "xerr.KindDatabase"
             | "xerr.KindShutdown" | "xerr.KindCircuitBreaker" ;
```

Case bodies are ordinary Flow expressions parsed with the normal precedence rules. The `else` case is optional and may appear at most once, last. Duplicate kind cases, unknown symbols, malformed case syntax, and missing closing braces are compile/parse errors rather than silently ignored cases. `? on_error` is recognized as this extension only when `on_error` is followed by `{`; all existing two- and three-arm `?` ternary forms remain unchanged.

Example (the guarded expression is deliberately domainless):

```nflow
fail @{ kind: "Timeout", message: "temporary processing failure" } ? on_error {
  xerr.KindTimeout -> match(.error.kind) {
    xerr.KindTimeout -> const @{ value: "deferred" },
    default -> const @{ value: "unexpected kind" }
  },
  xerr.KindNotFound -> const @{ value: "missing" },
  else -> const @{ value: "failed" }
} -> noop
```

## Runtime contract

- The guard wraps the complete preceding expression, whatever its domain or AST shape. It sees that expression's request input and error; it does not assume HTTP.
- A matching built-in `xerr.Kind` runs only its case. `xerr.Kind` is a string-backed, extensible Kernel type; classify with `xerr.From(err).Kind`: it preserves custom `*xerr.AppError` kinds and maps standard context deadlines/cancellation to `Timeout`/`Canceled`. `KindFrom` alone defaults raw standard context errors to `Internal`, so is insufficient here. A custom/unrecognized kind reaches `else`, if present. Without a matching case or `else`, return the original primary error unchanged.
- The recovery arm receives the original request as `input`, its original payload fields in a fresh local scope, `error.kind`, `error.message`, `error.original`, `error.cause`, and `error.details`, plus the symbolic `xerr.Kind…` values. Recovery-local bindings do not mutate shared or later invocations.
- A successful arm's value replaces the failed expression's value; normal downstream execution continues from that value. If the arm fails, expose no partial arm result and propagate an error that retains both the primary error and the handler error (`errors.Is`/`errors.As` can find either).
- An active caller context cancellation/deadline, a returned standard `context.Canceled`, or signal-driven cancellation is terminal: do not invoke an ordinary recovery arm. An explicit `xerr.Timeout` remains matchable. A timeout produced by an inner action may be recovered while the caller's outer context is still live; an expired outer context may not. Typed `xerr.Canceled` without standard context cancellation remains a matchable Kernel kind.
- `||` remains Kernel `FirstSuccessAny` catch-any fallback; its existing context check already stops fallback after caller-context cancellation. `@on_error` remains pipeline-wide target routing, but must apply the same terminal-cancellation gate.

## Reuse boundary

The existing `match` extension evaluates ordered expression conditions and `@on_error` uses the same expression language for routing. Share their condition compilation/selection helper with this guard and keep Kernel kind-symbol bindings in an extension. `||` has no predicates and is already implemented by Kernel's first-success combinator, so it continues to use that API rather than being translated into a third condition evaluator. The new postfix dispatch is generic parser plumbing; the AST node, Kernel-kind semantics, recovery environment, and terminal-error policy stay in the `on_error` extension.
