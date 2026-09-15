# Flow examples

Small, copy-pasteable flows that demonstrate every operator in the
flow language. Each example is a single file (plus `@include` /
`@require` where the feature needs more).

Run any of them with:

    nexss run ./examples/01_hello/main.flow -vvv '{"name":"world"}'

## Index

| Folder | Demonstrates |
|---|---|
| `01_hello`                | The smallest possible flow |
| `02_pipeline`             | `@pipeline` / `@end` — named subflow reuse |
| `03_parallel_and_fallback`| `&` scatter/gather, `\|\|` first-success |
| `04_conditional`          | `?` gate — run target only when gate is truthy |
| `05_loop`                 | `loop ... until ...` — bounded iteration |
| `06_include_action`       | `@include` composition, `@action` marker |
| `07_require`              | `@require` local and remote libraries |
| `08_typed_actions`        | Defining a typed action in Go |

For architectural demos (RL router, hot swap, evolutionary
optimizer, multi-tenant showcase) see `../showcase`.
