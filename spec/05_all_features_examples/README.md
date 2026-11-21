# All-features examples

This folder is a runnable, offline feature tour for the standard `nflow` CLI. Each top-level `.nflow` file is an independent asserted scenario; `modules/` contains an included source module, and `fixtures/` contains only static local JSON/JSONL data.

Run the full integrated suite from the repository root:

```sh
nflow test ./spec
```

The examples cover typed values and projections, current/original input and `as` bindings, pure functions, symbols, enums, macros, `@include`, schemas and typed pipeline boundaries, named pipeline arguments, conditionals, matches, `each`, bounded `loop`, parallel/race/gather, runtime and post-run assertions, both recovery forms, profiles and scopes, local pools/dispatch/fan-out, hooks, deterministic streams, compile-time config/constants/version checks, and JSON cleaning.

The tour intentionally avoids nondeterministic or time-dependent `env`, `uuid`, and `sleep`; approval gates that pause execution; external HTTP/process/WASM/provider actions; filesystem writes; and optional or self-test-only bundles. `@embed` also requires a host-mounted filesystem root. See the existing [resilience benchmark scenario](../04_resilience_benchmark/) for real local retry/timeout/breaker behavior and the [language reference](../../docs/language-reference.md) for the complete host-dependent and optional surface.
