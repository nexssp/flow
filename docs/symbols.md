# Flow symbol namespaces

Use symbols for domain values that must stay consistent across routing, output construction, and assertions. Declare each file-local value once, then reference it by an unquoted `namespace.name` expression.

```nflow
@symbols invoice {
  clear: "CLEAR"
  needs_review: "NEEDS_REVIEW"
  review_queue: "ap-exceptions"
  report_file: "reconciliation-report.json"
}

{ status: invoice.needs_review, queue: invoice.review_queue }
@assert: result.status == invoice.needs_review
```

## Declaration and scope

- Namespace and symbol names are identifiers: letters or `_` first, then letters, digits, or `_`.
- Values declared by `@symbols` are strings. Quote values containing spaces or punctuation.
- A namespace and each member may be declared only once in one effective flow. Duplicate namespace or member declarations fail preprocessing rather than silently choosing a winner.
- `@include` brings distinct namespaces from included files into the parent. Reusing a namespace across the parent and included files is an error, independent of include/declaration order.
- File-local declarations never mutate the process-global registry. A local namespace completely shadows a global namespace of the same name, including for direct data-value resolution.

Extensions can provide global namespaces through `Bundle.Symbols` or `compiler.RegisterNamespace`. The compiler's runtime symbol registry is the source used by expressions outside a file-local declaration.

## Where symbols resolve

The same local/global environment is available in action data, `match` and projection expressions, `loop(... ) until(...)`, the `assert(...)` keyword, `@assert:` checks, and error-routing conditions. For DSL data positions, a known bare `namespace.name` is replaced with its declared value. In expression positions, it is a variable/member lookup.

Symbols are not interpolated into quoted strings, embedded Python/Go source, shell text, or arbitrary external files. When a separate worker must use the same value, pass it as an argument or through a structured input from Flow. For example, a Python worker can read status values from `sys.argv` rather than carrying another copy of `"CLEAR"` and `"NEEDS_REVIEW"` in its code.

## Precedence and collision guidance

Expression environments are assembled in this order:

1. Registered global namespaces.
2. File-local namespaces, replacing a same-named global namespace.
3. The current input/result environment, which takes precedence on a key collision.

Avoid naming an input field the same as a symbol namespace. This keeps expressions deterministic and makes the symbol declaration the single source of truth for the domain value.

> A global namespace registration deliberately supports replacement for bundle reinitialization and tests. Bundles that need a shared namespace should coordinate its owner; registering the same name from multiple bundles makes registration order significant.
