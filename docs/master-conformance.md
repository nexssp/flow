# What the proposed master file actually supports

The conformance input in [`master_proposed.nflow`](../runner/testdata/master_proposed.nflow) migrates the old pipeline `:schema` alias to the explicit multiline `:request-schema` form. Its declaration prefix (through the boundary before section 8) compiles with multiline header modifiers. The full file executes through the real CLI; its eleven original post-run assertions pass.
The separate [`master_supported.nflow`](../runner/testdata/master_supported.nflow) remains a real native-runner/CLI input and standalone compatibility coverage. Its ten declared assertions check schema defaults, symbols and projections, unauthorized error recovery, a three-iteration loop, three collected ticks, a successful race, and predicate matching. It remains separate, standalone compatibility coverage.

## Construct-by-construct findings

| Supplied construct | Verified behavior |
|---|---|
| `@description`, `@nflow_version ">=0.0.0"` | Stock directives; accepted by the executable fixture. Version comparison follows the existing release/development-build rules, not a guarantee about a future runtime. |
| `@const DEFAULT_RETRY_COUNT = "2"` | Stock compile-time constant. The standalone compatibility fixture substitutes `${DEFAULT_RETRY_COUNT}` into its retry profile rather than leaving the declaration unused. |
| Scalar/block `@config` | Stock configuration metadata. An isolated runner probe with `@config { app_env: "production" }` followed by `{ env: .app_env ?? "fallback" }` returned `"fallback"` while compilation metadata retained `app_env: production`: config is not automatically projection input. The original `?? "production"` fallback would conceal that distinction. |
| `@symbols app_symbols` | Stock string namespace; `app_symbols.primary_queue` resolves during projection evaluation. The original numeric-looking `max_capacity: "100"` remains a string, not an integer declaration. |
| `@schema TelemetryBatch` and its Go-style tags | Stock schema declaration. The runnable fixture verifies required fields, `[]int` validation with `dive,gte=0`, `oneof=low normal high`, JSON field names, and missing-field defaults for sensor and priority at a pipeline boundary. This is not a promise that every Go tag or unknown future validation rule is supported. |
| `@profile` and `:parent=base_resilience` | Stock profile inheritance, using same-line or indented multiline header modifiers. The fixture exercises readable multiline profile and pipeline headers; the declaration prefix is compiled by test. The implementation merges parent settings then child overrides in [`scope/profile.go`](../extensions/scope/profile.go). |
| Zero-argument `@macro` containing `assert(...)` | Stock compile-time source expansion and runtime assertion. The standalone compatibility fixture expands and executes the original invariant before projection; it is not an AST-scope rewrite. |
| `@pipeline ... :request-schema=... :response-schema=... :profile=...` and `pipeline.NAME` | Stock declarations/calls. Request schema defaults and validation are applied before the pipeline body; response schemas validate results and preserve their original values. Any response-schema `default` tag is a compile-time error before execution. `!` independently rejects undeclared request or response fields. |
| Full resilience modifier chain | Stock modifiers: timeout, retry, cache, concurrency, rate limit, idempotency, deduplication, coalescing, tag and description. The regression tests cover mixed flags/values, compact and spaced spellings, multiline atom modifiers, quoted punctuation, and ternary separation. A native compilation test checks the exact reported `:rate_limit=50 :idempotent :dedup :coalesce` form. |
| `runtime.const @{ value: { ... } }` | Stock structured argument/object result. The fixture's resilience pipeline is compiled in the ordinary CLI run and separately invoked in the Go test with an explicit host idempotency store and request identity. That invocation proves composition/output, not every policy's timing, replay, or contention semantics. |
| `:auth :role="admin"`, `? on_error`, xerr kind arms, `error.reraise` | All are stock, including `xerr.KindUnauthorized`, `xerr.KindForbidden`, and `xerr.KindUnavailable`. With no authenticated identity, the executable fixture selects the Unauthorized arm. An isolated reraiser probe propagates the original Unavailable error. Supplying a real authenticated identity/roles is the host's responsibility. |
| `loop(...) until(.pending <= 0)` | Stock bounded loop. The executable fixture finishes with `pending=0`, `drained=3`. It does not introduce an unbounded while loop. |
| `realtime.ticks @{ hz: 100, max_catchup_ms: 20 } -> stream.take @{ count: 3 } -> stream.map @{ expression: "item" } -> collect` | The exact source/options are stock and executed by the fixture. The assertion checks three items, not exact wall-clock timing. `collect` is the compiler stream boundary; it is distinct from `stream.collect @{ limit: ... }`. |
| `parallel { label: ... }` and `race { ... }` | Stock Flow primaries, usable standalone or as projection property values. Control-level parallel/race retain Kernel cancellation and joining behavior; the standalone fixture retains compatibility coverage. |
| Nested `parallel`, `race`, and `match` projection values | Implemented as real registered Flow AST children: analyzed/built through Flow and lazily invoked by expr-lang when the enclosing property is evaluated. Each receives the same incoming projection root; sibling fields are not sequential dependencies and object fields are not implicitly concurrent. Typed errors/cancellation propagate. |
| `match(.peak) { > 90 -> ... }` | Supported subject-relative relational arms for `>`, `>=`, `<`, and `<=`, alongside existing equality-style subject matching and predicate match. The original full master exercises the syntax; its execution assertions are checked end-to-end. |
| Spread, field access, arithmetic, `len`, `max`, comparisons, and final projections | Stock expression features exercised across the original master's schema, macro, loop, matching and result branches. Its final projection successfully assembles the nested control results. |
| `@assert: result...` | Stock post-run checks, not compiler-only validation. The original master's eleven assertions and the standalone fixture's ten assertions are extracted from their actual metadata and evaluated against execution output. |

## Stock syntax versus host behavior

The original master uses `native.Bundles()` without extra extension bundles, credentials or external services. Its normal CLI and runner executions invoke the idempotent ingest branch successfully using the standard execution setup. A separate test supplies a local temporary idempotency store and request ID and invokes that pipeline directly, verifying explicit host wiring too. Authorization recovery is verified in the unauthenticated context only.

Tick scheduling, real authenticated identities, durable production storage, and the exact effects of resilience policies under load remain host/runtime concerns. The test is not a load or production-infrastructure certification.

## Implemented syntax improvements

[`compiler/parser.go`](../compiler/parser.go) now stops reading a modifier value when the next unquoted colon is immediately followed by an identifier, regardless of whether that next modifier has an equals sign. Quoted colons stay inside string tokens, and the existing whitespace-after-colon ternary rule is unchanged. Existing comma-valued modifiers are retained; the broader proposed scalar-only grammar was not adopted.

[`compiler/declaration_header.go`](../compiler/declaration_header.go) supplies a shared deterministic header grammar for profiles and pipelines. Modifiers may continue on lines indented deeper than the declaration, across blank lines and whole-line `#` or `//` comments. The first other significant line permanently starts the body; later atom modifiers are not stolen into the header. Flags, values, quoted punctuation and malformed headers are covered by parser and directive tests. Source lines are not folded or rewritten, and preprocessing retains the original line count.

Neither `@{}` interpolation nor lexical AST-scope changes were implemented. An isolated `runtime.const @{ value: "Maks" + "Julia" }` probe still fails with `expected ',' or '}', got "+"`; use the existing projection expression mechanism for calculation.

## Nested control and projection semantics

Projection composition preserves Flow expressions as registered AST children. They participate in analysis and build normally, and are invoked lazily by expr-lang when their enclosing value is evaluated; thus an unselected ternary arm is not called. Every nested child receives the same incoming projection root, not a preceding sibling property result. Expr-lang iterator variables such as `#item` are not automatically rebound as the control input. Control-level `parallel`/`race` use existing Kernel cancellation and join behavior; object properties are not implicitly concurrent. Nested typed errors and caller cancellation propagate. The surrounding expr-lang expression remains responsible for assembling the result object. This support does not imply interpolation in arbitrary `@{ ... }` data blocks, nor any load/timing or infrastructure certification.

## Run the checks

Use Go 1.27.3, as required by this checkout:

```sh
go test ./compiler ./extensions/scope ./extensions/pipeline -race -count=1
go test ./extensions/projection ./extensions/match ./runner -race -count=1
go test ./runner -run 'Test(NativeMaster|Master|OriginalMaster)' -count=1 -v
go run ./cmd/nflow run runner/testdata/master_proposed.nflow
go run ./cmd/nflow run runner/testdata/master_supported.nflow
go run ./cmd/nflow run extensions/projection/nflows/control_values.nflow
task ci
go test ./...
go vet ./...
```

The migrated conformance input was run through the real CLI; its output matches all eleven assertions. The runner conformance coverage exercises the full source and composition/error behavior. The separate `master_supported.nflow` remains standalone compatibility coverage. No load/timing or production-infrastructure certification is claimed.
