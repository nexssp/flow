# CLI and runtime

The `nflow` CLI compiles, runs, inspects, and packages `.nflow` files.
Install it with:

```sh
go install github.com/nexssp/flow/cmd/nflow@main
```

The examples below use `nflow` directly. From a source checkout, use
`go run ./cmd/nflow <args>` instead.

---

## Run a flow

```sh
nflow run flow.nflow
nflow run flow.nflow '{"user_id": 42}'
nflow run flow.nflow --assert='result.ok == true'
nflow run -vv flow.nflow
```

`run` accepts a `.nflow` file or an inline pipeline expression, an
optional JSON payload as a second argument (or on stdin), and two
flags:

| Flag | Effect |
|---|---|
| `-v`, `-vv`, `-vvv` | Increasing verbosity. `-vv` prints live action names; `-vvv` prints payloads. |
| `--assert=EXPR` | Evaluated after the run; exits 1 on failure. |
| `--info` | Describe the pipeline instead of executing it. |

If the file declares `@require`, the CLI builds or reuses a harness
binary that links the requested modules and runs the flow through it.
The harness is cached under `$XDG_CACHE_HOME/nflow/harness` (or the
platform equivalent); a source change to a local module invalidates the
entry automatically.

A successful run prints the flow's output. A failed run prints the
error and, at verbosity ≥ 1, an execution trace:

```
✗ Pipeline Execution Failed
  Error: [NotFound] capability http.request not registered

─── EXECUTION TRACE ──────────────────────────────────────────
  STATUS ACTION                          DURATION   ERROR
  ✓      runtime.const                   < 1µs      -
  ✗      http.request                    < 1µs      capability not registered
──────────────────────────────────────────────────────────────
         TOTAL                           < 1µs
```

---

## Check a flow without running it

```sh
nflow lint flow.nflow
nflow lint ./examples/...
nflow lint ./...
```

`lint` preprocesses each file, parses the pipeline, and validates every
atom and modifier against the active registry. It does not execute the
flow and does not fetch external payloads. It inherits every `@scope`,
`@profile`, and `@pipeline :profile` the runtime compiler would apply,
so a typo inside a scope block fails here the same way it fails at run
time.

Three invocation shapes:

| Target | Behavior |
|---|---|
| A file | Single-file lint; prints `ok` or the diagnostics |
| A directory | Recurses into `.nflow` files, sorted |
| `./...` or `./path/...` | Go-style recursive discovery |

Batch runs print one JSON array covering every file. A clean batch
prints `ok (N files)`.

Discovery skips `.git`, `.hg`, `.svn`, `vendor`, and `node_modules`,
does not follow directory symlinks, and does not follow file symlinks
during a walk. A direct single-file argument may name a symlink.

The exit code is 0 on success and 1 on any diagnostic. Diagnostics are
always JSON:

```json
[
  {
    "file": "flow.nflow",
    "kind": "unknown_modifier",
    "message": "modifier :timetout is not registered"
  }
]
```

The linter is the same compiler pipeline the runner uses; if `lint`
passes, the syntax and registry surface are correct. Runtime behavior
depends on the input, not the linter.

---

## Explain a flow

```sh
nflow explain flow.nflow
```

`explain` prints the effective policy per atom, with attribution to the
scope, profile, or pipeline that produced each modifier.

```
flow.nflow

  @pipeline fetch  [definition]
    pipeline.fetch
      :tag=api             from pipeline fetch
    http.request
      :timeout=5s          from profile reliable
      :retry=3             from profile reliable

  [top-level]
  pipeline.fetch  [invocation]
    (no modifiers)
```

Every `file:line` position that appears in an error is also printed here
in a form terminals can hyperlink. `explain` is a static report; it
does not execute the flow.

---

## Inspect the pipeline shape

```sh
nflow info flow.nflow
```

`info` prints the pipeline's atoms in order, the description, and any
`@assert` directives:

```
📋 FLOW INSPECTION: flow.nflow
════════════════════════════════════════════════════════════

Description: Extract and reshape user
Asserts: 1
  • result.name == "Maksymilian"

⚡ Pipeline:
  [1] http.request
  [2] { projection }
  [3] runtime.const
════════════════════════════════════════════════════════════
```

`info` is the quickest way to confirm which atoms a file actually calls
after preprocessing. It does not evaluate policies.

---

## Build a standalone executable

```sh
nflow build flow.nflow -o /tmp/my-flow
/tmp/my-flow
```

`build` produces a self-contained binary that embeds the `.nflow`
source and any `@require` modules. The binary runs the flow when
launched, accepts the same JSON payload and CLI flags as `nflow run`,
and prints the same output.

Options:

| Flag | Effect |
|---|---|
| `-o FILE` | Output path. Defaults to the source filename without extension. |

`-o` must come before the input path. The current parser stops reading
flags when it reaches the positional argument, so
`nflow build flow.nflow -o out` does not set the output.

The build runs `go mod tidy -e` and `go build` in a temporary directory
with the source and a generated `main.go`. It targets the host
platform; for cross-compilation, invoke `go build` on the generated
harness yourself with `GOOS` and `GOARCH` set.

---

## List and show capabilities

```sh
nflow list
nflow list fs
nflow list --json > catalog.json
```

`list` prints every atom, source, operator, modifier, and directive the
compiler knows about. A pattern filters by substring on name,
description, or tag. `--json` emits the catalog in machine-readable
form. `--flow FILE` loads `@require` bundles from `FILE` before listing,
so the surface matches what that flow sees.

```sh
nflow show fs.walk
```

`show` prints details for one capability: description, tags, scope,
typed request and response fields, and the example payload if the
bundle declares one. It accepts the same `--flow` flag.

```sh
nflow catalog > catalog.json
```

`catalog` is `list --json` without the rendering path. Use it when
you want the full surface as JSON.

---

## Self test

```sh
nflow self test
```

Every bundle ships its own coverage: inline features and `.nflow`
fixtures. `self test` runs them all in-process and reports a pass/fail
line per feature:

```
▸ MACROS  13 features
  ✓  basic expansion                    1.02ms   126c 10r
  ✓  parameterless expansion            0ns      125c 10r      macros/nflows/basic.nflow
  ...
```

Narrow with one or more case-insensitive substring filters:

```sh
nflow self test macros           # features whose section or name contains "macros"
nflow self test loop assert      # OR of both filters
```

Options:

| Flag | Effect |
|---|---|
| `--verbose` | Print the DSL for failing features |
| `--json` | Emit a machine-readable report instead of the live checklist |
| `--save-baseline` | Write `.nflow_selftest_baseline.json` for regression detection |
| `--no-color` | Disable ANSI output |

Exit code is 0 only when every selected feature passes.

---

## Rebuild the CLI in place

```sh
nflow self up
```

`self up` rebuilds `nflow` from the current source tree with VCS
metadata embedded. It writes to `$GOBIN` or `$GOPATH/bin`. Useful
during development when you want the version output to reflect the
source you just changed.

---

## Shell completion

```sh
nflow completion bash > ~/.local/share/bash-completion/completions/nflow
nflow completion zsh  > "${fpath[1]}/_nflow"
nflow completion pwsh | Out-String | Invoke-Expression
```

Completion scripts are static: they list commands and flags, not
registry contents. For capability completion, use `nflow list` and feed
the output into your editor's completion source.

---

## Version

```sh
nflow version
```

Prints version, commit hash, build time, Go version, and platform. When
the binary was installed with `go install` or built without linker
flags, the values come from the module and VCS metadata Go embeds
automatically.

---

## Extension bundles

A `.nflow` file loads external code with `@require`:

```nflow
@require github.com/example/fancy v1.2.3
@require ./local/helpers { prefix: "DEMO" }
```

If any required module is not linked into the current binary, `run`,
`lint`, `list`, `show`, and `explain` build a harness binary that
links exactly the declared modules. The harness is cached by content
hash: identical module sets with identical source reuse the same
binary. Change the source of a local module and the cache entry is
invalidated on the next run.

Bundle options declared in `{ ... }` are validated by the bundle. An
unrecognized key is a compile error:

```
@require ./helpers { preifx: "DEMO" }
  error: @require ./helpers: unknown option "preifx" (accepted: prefix)
```

See [extensions.md](extensions.md) for the bundle contract.
