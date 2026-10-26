# `extensions/lang` integration notes

The language bundle is integrated in this repository; no standalone build tag or separate module is needed.

## How the bundle is wired

`library.go` embeds the runner profiles and fixtures, registers the `lang` bundle with the compiler, exports the runner source paths, and adapts language execution to `extensions/external`. `@require lang` enables its language keywords, directives, and preflight validator for a flow. The bundle is intentionally absent from `native.Bundles()`; it is included in the repository self-test bundle set so its conformance fixtures run.

The compiler invokes the bundle's `OnPreprocess` hook before parsing. That hook contributes the language keyword primaries and an AST validator. The validator gathers every language block and checks interpreter availability before any action executes. The normal preflight is find-only: it does not install software automatically.

Inline source is materialized into a per-invocation temporary file. A `<language>.file "path"` form resolves the script beside the `.nflow` file and passes that path as a single argument, preserving the original file location and imports. Add `:autocreate` to that call to create a missing script from the runner's `file_template`; creation is exclusive and never overwrites a file. The template contract is JSON on stdin and JSON on stdout. The language process runs with the host process's permissions; this bundle does not provide a sandbox.

For example, `@require lang` enables `{ samples: [...] } -> py.file "./analysis.py"`. The script reads the incoming JSON value from stdin and writes the next JSON value to stdout. `@include` remains specific to `.nflow` source modules.

## Inline definitions

`@lang.runner` and `@lang.define` accept strict JSON objects with an `id`, file extension, aliases, and interpreter lookup commands. Definitions remain in a process-wide registry after compilation. Long-running hosts must not recompile definitions with the same ID or alias; use unique definitions or register runners once in host code.

## Conformance tests

Run the focused package and integration tests with:

```sh
go test ./extensions/lang ./cli/selftest ./native ./extensions/external ./extensions/include
```

Run the full repository suite with:

```sh
go test ./...
```

Fixtures for languages unavailable on the current machine are reported as skipped by the self-test runner; other compile or execution errors remain failures. The standard Go and Python fixtures run when those interpreters are present. Rust and TypeScript fixtures require `rust-script` and a configured TypeScript runner, respectively.
