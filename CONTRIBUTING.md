# Contributing to Nexss Flow

Nexss Flow is the DSL, compiler, runtime, and extension system in the Nexss ecosystem. Keep changes focused on correctness, clarity, portability, and measured performance. Avoid expanding the public API without a clear need.

## Before opening a pull request

Use Go 1.26 or newer. Run Task commands from the repository root (`flow/`). For a quick local test pass:

```bash
task test
```

The full local check gate is:

```bash
task check
```

It checks formatting without rewriting files, then runs `go vet`, `golangci-lint`, and the full race-enabled Go test suite. The configured formatters are gofumpt and goimports. To check formatting alone, use `task fmt:check`; to apply formatting, use `task fmt` and review the resulting diff.

Useful focused commands include:

```bash
task build
task build:binary
task test:race
task test:smoke
task self
task examples
```

`task examples` runs the curated runtime smoke checks: six introductory flows in `examples/00_flow_basics/` and four practical secure-pipeline flows in `examples/01_secure_pipeline/`.

The optional benchmark task is:

```bash
task bench
```

There are currently no Go `Benchmark...` functions, so this runs no substantive benchmark cases until benchmark functions are added. When adding benchmarks or making a performance claim, report the Go version and hardware used.

## Flow CLI and maintenance tasks

Run or lint a Flow file with, for example:

```bash
task run -- examples/00_flow_basics/04_data_transformation.nflow
task lint:nflow -- examples/00_flow_basics/04_data_transformation.nflow
```

Create an extension bundle with the CLI-backed task (use a valid Go package name that does not already exist):

```bash
task ext:new -- my_extension
```

`task tidy` tidies only the Flow module, so it also works in a standalone clone. If a sibling `../kernel` checkout is present and its module path is `github.com/nexssp/kernel`, `task tidy:kernel` is available as a separate opt-in. `task clean` removes only the generated `bin/nflow`/`bin/nflow.exe` binary and `catalog.json`; it does not clear shared Go build or test caches.

## Design and review expectations

- Public API changes should explain compatibility impact and include tests.
- New dependencies should have a clear need and license review; keep provider-specific integrations in an adapter when that is the better boundary.
- Keep Flow's core and built-in extensions reusable. Application-specific business rules, hosted-service integrations, and provider-specific behavior generally belong in downstream bundles or adapters.
- Add or update `.nflow` examples when syntax or user-facing behavior changes, and run documented runnable flows through the current CLI.

## Optional Git hooks

Install the configured pre-commit (or [prek](https://github.com/j178/prek) ) and pre-push hooks with [pre-commit](https://pre-commit.com/):

```bash
pre-commit install --hook-type pre-commit --hook-type pre-push
```

The pre-commit stage runs whitespace, end-of-file, YAML, large-file, formatter, lint, and Go test checks. The pre-push stage also runs `go vet` and race-enabled Go tests. Hooks are a local safeguard, not a replacement for `task check` or CI.

Thank you for contributing to Nexss Flow.
