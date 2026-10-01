# Nexss Flow

Nexss Flow is a Go runtime and language for composing `.nflow` programs from registered actions. The `nflow` CLI runs flows and inspects the active compiler surface.

## Install

Requires Go 1.26 or later. Install the CLI from the current `main` branch:

```sh
go install github.com/nexssp/flow/cmd/nflow@main
nflow version
```

Make sure Go's binary directory (`$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH`.

## Quick start

```sh
nflow help
nflow lint examples/00_flow_basics/04_data_transformation.nflow
nflow lint ./examples/...
nflow run examples/00_flow_basics/04_data_transformation.nflow
```

The example projects a nested value into a new shape and prints:

```json
{
  "id": 101,
  "name": "Ada",
  "status": "active",
  "summary": "Ada (ID: 101) is active"
}
```

## Examples

The introductory flows are checked by CI: each one must pass `nflow lint` and run successfully from the repository root.

- [Logging and debug](examples/00_flow_basics/01_logs_and_debug.nflow)
- [Timeout and fallback](examples/00_flow_basics/02_sleep_and_timeout.nflow)
- [Dynamic dispatch](examples/00_flow_basics/03_dynamic_routing.nflow)
- [Data transformation](examples/00_flow_basics/04_data_transformation.nflow)
- [Error fallback](examples/00_flow_basics/05_error_fallback.nflow)
- [Parallel actions](examples/00_flow_basics/06_parallel_actions.nflow)

Run the complete set locally with `task examples` or run an individual flow with `nflow run <file.nflow>`.

## Verify without running a flow

Use `nflow lint <file.nflow>` for a static check, or pass a directory or Go-style recursive target such as `nflow lint ./examples/...` to discover sources automatically. It preprocesses each file, parses its pipeline, and checks referenced atoms and modifiers against that file's active registry; it does **not** execute flows. Recursive discovery sorts paths, skips `.git`, `.hg`, `.svn`, `vendor`, and `node_modules`, and does not follow symlinks. Lint is not a substitute for exercising runtime behavior, and it does not prove every expression or input will succeed. The broader `nflow lint ./...` also discovers internal developer sources and extension fixtures, some of which currently produce context-specific diagnostics; CI intentionally scopes its lint gate to `./examples/...`.

The similarly named build commands do different jobs:

- `nflow build -o ./bin/my-flow examples/00_flow_basics/04_data_transformation.nflow` creates a standalone executable with the flow embedded. It is a packaging/build command, not a check-only validator; the flow runs when you launch the generated executable. Put `-o` before the input path with the current CLI parser. `nflow help build` currently advertises `--target`, but this checkout's implementation does not parse that flag; the command above uses the supported `-o` option.
- `go build ./...` compiles the repository's Go packages. It does not validate or execute `.nflow` files.
- `go test ./...` runs the repository's Go tests, including tests that may exercise Flow features. For a no-runtime-execution check of a particular flow, use `nflow lint`; for repository code verification, use `go test ./...`.

## Documentation

- [Language syntax and directives](docs/language.md)
- [CLI and runtime](docs/cli.md)
- [Extension bundles](extensions/README.md)
- [Practical secure-pipeline examples](examples/01_secure_pipeline/README.md)
- [Editor setup for VS Code and Zed](docs/editors.md)

## Microkernel and extensions

Nexss Flow uses a microkernel-style design: `core.Bundle` is the contract through which bundles contribute action libraries and compiler extensions. The CLI assembles its built-in bundles, while `@require` can load a bundle for a flow; a package's presence under `extensions/` alone does not enable it. See the [extension guide](extensions/README.md) for the current bundle model.
