# CLI and runtime

The CLI is built in this repository as `./cmd/nflow`. The commands below use `go run` from the repository root; after installing the CLI, the same arguments can be passed directly to `nflow`.

## Run and check a flow

```sh
go run ./cmd/nflow help
go run ./cmd/nflow help run
go run ./cmd/nflow run examples/00_flow_basics/04_data_transformation.nflow
go run ./cmd/nflow run examples/00_flow_basics/04_data_transformation.nflow --assert='result.id == 101'
go run ./cmd/nflow lint examples/00_flow_basics/04_data_transformation.nflow
go run ./cmd/nflow lint ./...
go run ./cmd/nflow lint ./examples/...
```

`run` accepts a `.nflow` path and an optional JSON payload. Its documented flags include `-v`/`-vv`/`-vvv` for verbosity and `--assert=EXPR` for a post-run result check. `lint` checks a flow against the active registry without executing it. Pass a file to retain the single-file check, an exact directory to check its sources recursively, or a Go-style `./...` / `./path/...` target. Multiple file, directory, and recursive targets may be supplied together. Quote or otherwise pass `./...` as a literal argument; shells do not expand this pattern.

Directory discovery checks `.nflow` files in deterministic path order. It skips VCS metadata directories (`.git`, `.hg`, `.svn`), vendored dependencies (`vendor`), and installed JavaScript dependencies (`node_modules`). It does not follow symlinked directories and skips symlinked files during discovery, avoiding loops and duplicate paths. A direct single-file argument may still name a symlink. If a selected directory contains no `.nflow` files, lint reports that explicitly and exits nonzero. Batch failures return one JSON diagnostics array covering the discovered sources that failed; an all-valid batch prints `ok (N files)`.

Use `nflow lint ./examples/...` (or `go run ./cmd/nflow lint ./examples/...` from this checkout) to check every user-facing `.nflow` example; CI uses this scoped command. The broader `nflow lint ./...` remains available and also discovers internal developer sources under `nflows/` and extension test fixtures. Some of those files currently produce context-specific parse or registry diagnostics, so a full-tree failure does not imply that the user-facing examples failed. CI intentionally scopes its lint gate to `examples/` rather than adding global discovery exclusions.

## Build a standalone executable

```sh
go run ./cmd/nflow build -o /tmp/flow-app examples/00_flow_basics/04_data_transformation.nflow
/tmp/flow-app
```

Put `-o FILE` before the input path. The current build parser stops reading flags when it reaches the positional path, so putting `-o` after the path does not select the requested output.

`nflow build` currently targets the host platform. It does not provide a `--target` cross-compilation flag; for cross-compilation, build the generated Go harness with the required `GOOS` and `GOARCH` values.

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
