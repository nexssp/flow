# Contributing to Nexss Ecosystem

This repository is part of the **Nexss Ecosystem**. Keep packages focused and composable. Changes should improve correctness, clarity, portability, or measured performance without expanding the public API unnecessarily.

## Toolchain

Use the Go version declared in `go.mod`, [Task](https://taskfile.dev), and the golangci-lint version pinned in [CI](.github/workflows/ci.yml). The root module contains `compiler`, `builder`, `extensions`, `runner`, `native`, and `cli`; these are package boundaries, not separately released modules.

Install Task once:

```sh
go install github.com/go-task/task/v3/cmd/task@latest
```

Make sure Go's binary directory is on `PATH`. `task --list` shows the current targets. `task setup` fetches missing repository templates and installs gotestsum and govulncheck; it does not install golangci-lint. Install that tool separately using the version pinned in CI.

## Before opening a pull request

The normal full verification workflow is:

```sh
task tidy       # update go.mod / go.sum; inspect and commit any required changes
task fmt        # auto-format Go sources; inspect the resulting diff
task pre-push   # extension self-tests, lint, short race tests
task ci         # extension self-tests, integrated specs, lint, uncached race tests
```

`task ci` runs its dependencies in parallel. Formatting is a separate operation, not a dependency of these gates. There is no `ci:pr`, `fmt:check`, `examples`, or `test:harness` target in this checkout.

Useful focused checks:

```sh
task lint -- ./compiler ./runner
task test -- ./compiler -run '^TestParser' -count=1
task test:race:nocache -- ./runner -run '^TestApproval'
task lint:nflow # static lint of the in-repository spec/ sources
task specs      # execute the integrated acceptance flows and their assertions
task self       # native and optional-bundle extension conformance checks
task test:smoke # CLI smoke checks
task vuln       # govulncheck
```

For a deliberately scoped review, select explicit packages and test names rather than invoking the full gates. Record the commands, results, and untested scope. Do not describe focused checks as a full-suite pass.

The `examples/` directory points to the central [examples repository](https://github.com/nexssp/examples). Run its example targets from that checkout, not this one. In this repository, use `task specs` and `task lint:nflow` when changing DSL composition or CLI wiring. For build or `@require` changes, select the relevant tests in `builder/` and `cli/`; these can compile additional binaries.

## Requirements

- Public-interface changes need a compatibility explanation and tests.
- Performance claims need a benchmark with the Go version and hardware documented.
- New dependencies need a reason, license review, and evidence that they do not belong in an adapter package instead.
- Keep business rules, transports, hosted services, and provider-specific behavior outside the compiler core.
- Add DSL features through the extension/bundle mechanism; scaffold with `task ext:new -- <go_package_name>`.
- Reuse Kernel primitives and `xtest` / `xtest/ktest`; do not add parallel helpers or `testify`.

## Local pre-commit hooks

```sh
pre-commit install
# or
prek install
```

The hooks call `task pre-commit` and `task pre-push`, which run broad checks. Task and golangci-lint must be available on `PATH` first. Do not trigger these hooks as part of a review explicitly restricted to focused checks.

If a core or Kernel change appears necessary, write a short RFC describing the missing contract, alternatives, and why an existing extension or adapter cannot solve it.
