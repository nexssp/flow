# CLI and runtime

The CLI is built in this repository as `./cmd/nflow`. The commands below use `go run` from the repository root; after installing the CLI, the same arguments can be passed directly to `nflow`.

## Run and check a flow

```sh
go run ./cmd/nflow help
go run ./cmd/nflow help run
go run ./cmd/nflow run examples/00_flow_basics/04_data_transformation.nflow
go run ./cmd/nflow run examples/00_flow_basics/04_data_transformation.nflow --assert='result.id == 101'
go run ./cmd/nflow lint examples/00_flow_basics/04_data_transformation.nflow
```

`run` accepts a `.nflow` path and an optional JSON payload. Its documented flags include `-v`/`-vv`/`-vvv` for verbosity and `--assert=EXPR` for a post-run result check. `lint` checks a flow against the active registry.

## Build a standalone executable

```sh
go run ./cmd/nflow build -o /tmp/flow-app examples/00_flow_basics/04_data_transformation.nflow
/tmp/flow-app
```

Put `-o FILE` before the input path. The current build parser stops reading flags when it reaches the positional path, so putting `-o` after the path does not select the requested output.

## Inspect the active compiler surface

```sh
go run ./cmd/nflow list
go run ./cmd/nflow show timeout
go run ./cmd/nflow catalog
```

`list` displays known atoms, modifiers, directives, and operators; it accepts a search pattern and supports `--json`. `show` prints details for one catalog entry. `catalog` emits the compiler surface as JSON. Use `go run ./cmd/nflow help <command>` for current command usage and options.

## Bundles and feature tests

The CLI assembles its built-in bundles when it starts. A flow that declares `@require` may cause `nflow run` to build or reuse a harness for the required bundle. To run the embedded feature coverage suite, use:

```sh
go run ./cmd/nflow self test
```

For bundle structure and activation, see the [extension guide](../extensions/README.md). For the language forms used by `.nflow` files, see [language syntax](language.md).
