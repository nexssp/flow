# RFC: prepared source for shared compilation

## Missing contract

`runner.Compile` must expose root-source metadata to materializers before it compiles the top-level program. Today it calls `core.Preprocess`, then passes raw source to `core.CompileAction`, which preprocesses the same source again. Passing cleaned text instead would discard metadata and could change directive-derived strictness, config, macros, schemas, and extension contributions.

## Proposed change

Add an opaque `core.PreparedSource` produced by a small core preparation function that applies compile-option config and preprocesses once. Extend `CompileReq` to optionally carry that prepared value. `CompileAction` will route both raw requests and prepared requests through its existing shared parse/analyze/build implementation; raw requests prepare internally. The runner will prepare once, expose the prepared metadata to materializers, and pass that exact source result to `CompileAction`. Pipeline bodies remain independent raw sources and are compiled separately.

## Alternatives

Passing only cleaned source loses metadata and original source identity. Keeping the duplicate preprocessing preserves behavior but repeats directive callbacks and filesystem work. Moving materialization into extensions cannot close the gap: `OnPreprocess` contributes compile configuration inside compilation, while runner materializers need the root metadata and a compiler callback before top-level compilation; that ordering is generic runner/compiler orchestration, not an extension-owned language feature.

## @require bootstrap scan

Bundle resolution needs the complete `@require` set before the final directive table and compiler configuration can be built, so this discovery cannot be removed by the prepared-source API. Keep it explicitly separate from compilation preprocessing. A focused scanner is acceptable only if it delegates declaration parsing and option/error semantics to the existing `@require` directive handler and preserves include behavior; otherwise retain the bootstrap preprocessing until such a scanner can be implemented without a second grammar.
