# Nexss Flow

Nexss Flow is a Go runtime and language for composing `.nflow` programs from registered actions. The `nflow` CLI runs flows and inspects the active compiler surface.

## Quick start

Requires Go 1.26 or later. From the repository root:

```sh
go run ./cmd/nflow help
go run ./cmd/nflow run examples/00_flow_basics/04_data_transformation.nflow
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

These introductory examples currently run successfully from the repository root:

- [Logging and debug](examples/00_flow_basics/01_logs_and_debug.nflow)
- [Timeout and fallback](examples/00_flow_basics/02_sleep_and_timeout.nflow)
- [Dynamic dispatch](examples/00_flow_basics/03_dynamic_routing.nflow)
- [Data transformation](examples/00_flow_basics/04_data_transformation.nflow)

## Documentation

- [Language syntax and directives](docs/language.md)
- [CLI and runtime](docs/cli.md)
- [Extension bundles](extensions/README.md)

## Microkernel and extensions

Nexss Flow uses a microkernel-style design: `core.Bundle` is the contract through which bundles contribute action libraries and compiler extensions. The CLI assembles its built-in bundles, while `@require` can load a bundle for a flow; a package's presence under `extensions/` alone does not enable it. See the [extension guide](extensions/README.md) for the current bundle model.
