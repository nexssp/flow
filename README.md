# 🌀 Nexss Flow

[![CI](https://github.com/nexssp/flow/actions/workflows/ci.yml/badge.svg)](https://github.com/nexssp/flow/actions/workflows/ci.yml)
[![Latest tag](https://img.shields.io/github/v/tag/nexssp/flow?sort=semver&label=latest%20tag)](https://github.com/nexssp/flow/tags)
[![Go Reference](https://pkg.go.dev/badge/github.com/nexssp/flow.svg)](https://pkg.go.dev/github.com/nexssp/flow)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

Runnable examples are maintained in the central [Nexss Ecosystem Examples repository](https://github.com/nexssp/examples/tree/main/flow).

Nexss Flow is a Go runtime and language for composing `.nflow` programs from registered actions. The `nflow` CLI runs flows and inspects the active compiler surface.

## Install

Install the CLI from the latest tagged release:

```sh
go install github.com/nexssp/flow/cmd/nflow@latest
nflow version
```

Make sure Go's binary directory (`$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH`.

## Quick start

From the central examples checkout:

```sh
git clone https://github.com/nexssp/examples.git
cd examples
nflow lint flow/00_flow_basics/04_data_transformation.nflow
nflow run flow/00_flow_basics/04_data_transformation.nflow
```

The example projects a nested value into a new shape and prints:

```json
{
  "id": 101,
  "name": "Maksymilian",
  "status": "active",
  "summary": "Maksymilian (ID: 101) is active"
}
```

## Examples

The introductory flows are checked by CI in the central examples repository: each one must pass `nflow lint` and run successfully from the examples checkout.

- [Logging and debug](https://github.com/nexssp/examples/tree/main/flow/00_flow_basics/01_logs_and_debug.nflow)
- [Timeout and fallback](https://github.com/nexssp/examples/tree/main/flow/00_flow_basics/02_sleep_and_timeout.nflow)
- [Dynamic dispatch](https://github.com/nexssp/examples/tree/main/flow/00_flow_basics/03_dynamic_routing.nflow)
- [Data transformation](https://github.com/nexssp/examples/tree/main/flow/00_flow_basics/04_data_transformation.nflow)
- [Error handling and retry](https://github.com/nexssp/examples/tree/main/flow/00_flow_basics/06_error_handling_and_retry.nflow)
- [Parallel actions](https://github.com/nexssp/examples/tree/main/flow/00_flow_basics/07_parallel_actions.nflow)

Run the complete set locally with `task examples:nflow` from the central examples repository, or run an individual flow with `nflow run <file.nflow>`.

## Integrated specifications

The root [`spec/`](spec/README.md) suite contains deterministic, real-world workflows that combine extensions with scenario-local custom bundles. It covers order intake, support triage and safe recovery, and payment reconciliation. Run its lint gate with `task lint:nflow`; `task test` and `task ci` also run every extension's categorized NFlow conformance suite.

## Verify without running a flow 🌀

Use `nflow lint <file.nflow>` for a static check, or pass a directory or Go-style recursive target such as `nflow lint ./flow/...` to discover sources automatically. It preprocesses each file, parses its pipeline, and checks referenced atoms and modifiers against that file's active registry; it does **not** execute flows. Lint is not a substitute for exercising runtime behavior, and it does not prove every expression or input will succeed.

The similarly named build commands do different jobs:

- `nflow build -o ./bin/my-flow flow/00_flow_basics/04_data_transformation.nflow` creates a standalone executable with the flow embedded. It is a packaging/build command, not a check-only validator; the flow runs when you launch the generated executable. Put `-o` before the input path with the current CLI parser. `nflow help build` currently advertises `--target`, but this checkout's implementation does not parse that flag; the command above uses the supported `-o` option.
- `task build` compiles the repository's Go packages. It does not validate or execute `.nflow` files.
- `task test` runs the repository's short test suite. Use `task test -- ./runner` to target a package, `task test:race` for race detection, and `task ci` for the complete repository gate.

## Repository workflows

All local verification goes through [Task](https://taskfile.dev), matching CI:

```sh
task --list
task test
task test -- ./runner
task lint:nflow
task test:smoke
task bench
task ci
```

## Documentation

- [Language syntax and directives](docs/language.md)
- [Symbol namespaces and single-source values](docs/symbols.md)
- [CLI and runtime](docs/cli.md)
- [Extension bundles](extensions/README.md)
- [Durable runner execution](docs/runner-durability.md)
- [Approval pause and resume](docs/approval-resume.md)
- [Resilience policies](docs/resilience.md)
- [Error recovery](docs/on-error.md)
- [Schemas and validation](docs/schema.md) and [runtime validation](docs/validation.md)
- [Integrated specifications](spec/README.md)
- [Practical secure-pipeline examples](https://github.com/nexssp/examples/tree/main/flow/01_secure_pipeline/README.md)
- [Editor setup for VS Code and Zed](docs/editors.md)

## Microkernel and extensions

Nexss Flow uses a microkernel-style design: `compiler.Bundle` is the contract through which bundles contribute action libraries and compiler extensions. The CLI assembles its built-in bundles, while `@require` can load a bundle for a flow; a package's presence under `extensions/` alone does not enable it. See the [extension guide](extensions/README.md) for the current bundle model.
