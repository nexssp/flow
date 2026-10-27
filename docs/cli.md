# CLI and runtime

The `nflow` CLI compiles, runs, inspects, and packages `.nflow` files.
Install it with:

```sh
go install github.com/nexssp/flow/cmd/nflow@latest
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
optional JSON payload as a second argument (or on stdin), and the
following flags:

| Flag | Effect |
|---|---|
| `-v`, `-vv`, `-vvv` | Increasing verbosity. `-vv` prints live action names; `-vvv` prints payloads. |
| `--assert=EXPR` | Evaluated after the run; exits 1 on failure. |
| `--info` | Describe the pipeline instead of executing it. |
| `--journal-dir DIR` | Persist completed capability results (and idempotency responses) below `DIR`. Prints the execution ID to stderr for later replay. |
| `--execution-id ID` | Replay/resume using an existing journal identity; requires `--journal-dir`. |
| `--approval TOKEN` | Approve named `@approval` gates (comma-separated names or `all`). See [approval pause and resume](approval-resume.md). |

For example, run once and save the reported execution ID, then supply that
same ID with the same source name and journal directory to replay completed
steps:

```sh
nflow run flow.nflow --journal-dir .flow-state
# Replace this example ID with the exact value printed above.
nflow run flow.nflow --journal-dir .flow-state --execution-id flow-0123456789abcdef0123456789abcdef
```

Use a new execution ID when the workflow changes. Keep the source name and
callsite layout stable when resuming; a matching ID can replay a result
recorded for the same source position. A FileJournal is local to one process
for idempotency claim coordination and is not an execution-wide or distributed
lock service. Do not run the same execution ID concurrently, even within one
process, when repeating side effects would be unsafe.

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

## Format Flow source

```sh
nflow fmt flow.nflow
nflow fmt --write flow.nflow
nflow fmt --check flow.nflow
cat flow.nflow | nflow fmt --stdin
```

`nflow fmt` is the canonical formatter used by editor integrations. With one
named file and no mode flag it writes formatted source to stdout; with multiple
files, choose `--write` or `--check`. `--write` atomically replaces valid files
only. `--check` reports any file that differs and exits non-zero without writing.
With no file argument, `-`, or `--stdin`, the command reads stdin and writes
formatted source to stdout. Conflicting or ambiguous modes are rejected.

Parser failures include a file, line, and column when available; invalid files
are never rewritten. LSP-based editors receive the same source-positioned errors
as diagnostics. Formatting preserves comments, strings, opaque expression text,
and modifier order. It is not a substitute for `nflow lint`: use lint to validate
the fully preprocessed source, active registries, atom names, and modifiers.

---

## Check a flow without running it

```sh
nflow lint flow.nflow
nflow lint ./flow/...
```

From the central examples checkout, run the same command against its `flow/` tree.

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
nflow build -o /tmp/my-flow flow.nflow
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
| `--debug`, `-d` | Keep debug symbols. |
| `--no-trimpath` | Keep full source paths in the built executable. |
| `--all-bundles` | Link all native bundles rather than the planned subset. |

Build flags may precede or follow the input path. `--output FILE` and
`--output=FILE` are also accepted; an empty or missing output value is an
error. The output must not refer to the source workflow, including via a
symlink or hard link.

The build runs `go mod tidy` and `go build` in a temporary directory
with the source and a generated `main.go`. Dependency-resolution errors
stop the build. It defaults to the host platform; for cross-compilation,
set `GOOS` and `GOARCH` when invoking `nflow build`. The temporary build
directory is removed when the command returns.

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

Exit code is 1 when a selected feature fails and 2 when no features match.
Explicitly skipped features do not make the CLI return a failure; the
repository's `TestAllNFlowConformance` gate below additionally requires zero
skips and a passing result for every discovered feature.

When changing fixture discovery, inventory wiring, or parser syntax in this
repository, these focused Go checks exercise the corresponding gates without
running the full repository suite:

```sh
go test ./native ./cli/selftest -run '^(TestSelftest_|TestAllNFlowConformance$)' -count=1 -v
go test ./extensions/embed -run '^TestHostRootNFlowFixture$' -count=1
go test ./compiler -run '^Test(Parse|Parser|SubParse|ApplyAll|ValidateModifier|ModifierTable|NewModifierTable|DiscoverFixtures)' -count=1
```

Run them from the repository root with the Go version specified in `go.mod`.
`TestAllNFlowConformance` executes the same native/optional-bundle fixtures as
`nflow self test`, and additionally rejects skipped or missing results. The
separate embed test supplies the host filesystem root that its fixture needs.
Use `task specs` to execute integrated acceptance scenarios and `task lint:nflow`
for their static lint gate.

---

## Benchmark built-in scenarios

```sh
nflow self bench [filter...] [--path FILE_OR_DIR] [--iterations N] [--warmup N] [--json] [--list]
```

`self bench` measures whole `.nflow` scenarios, reusing the sampling and
latency-summary primitive used by `bench.run`; it does not benchmark the
`bench.run` action itself and is not a correctness gate. Use `nflow self test` for feature-correctness coverage.
Benchmarks run locally and the built-in scenarios do not call external services.

With no `--path`, the command discovers embedded scenarios recursively from
`cli/selfbench/scenarios`, including every `*.nflow` file. Add a scenario there
as a `.nflow` file (or nested folder) to include it in the built-in set; rebuild
the CLI for that embedded change to take effect. Alternatively, `--path` selects
a single file or a directory on disk and **replaces**, rather than extends, the
default embedded set. Custom scenarios can therefore be benchmarked without
rebuilding:

```sh
nflow self bench --path ./bench-flows
nflow self bench --path ./bench-flows/parse.nflow parse
```

Zero or more filters match case-insensitive substrings in scenario paths,
relative to the selected scenario root; multiple filters are ORed. For example,
`nflow self bench parse setup` selects paths containing either `parse` or
`setup`. `--list` prints the selected scenarios without compiling or executing
them.

| Option | Default and behavior |
|---|---|
| `--path FILE_OR_DIR` | Use one custom `.nflow` file or recursively discover `*.nflow` files below a directory instead of embedded scenarios. |
| `--iterations N` | Number of measured warm iterations; default `50`, allowed range `1..1000000`. |
| `--warmup N` | Number of unreported warmup iterations; default `3`, allowed range `0..1000000`. |
| `--json` | Emit the benchmark report as JSON. |
| `--list` | List matching scenario paths and do not execute them. |

For each scenario, `compile_ms` measures in-process compile/setup time and
`first_run_ms` measures its first execution. **`first_run_ms` is not an OS or
process cold-start measurement.** Warm-run metrics exclude warmup and report
minimum, maximum, mean, p50, p95, and p99 latency, runs per second (`rps`), and
errors. Each scenario is compiled once; repeated executions reuse its compiled
program and any retained cache/state, but receive fresh payload maps and execution
identities. Timed samples include the normal runner execution wrapper; they are
not isolated action-call timings. `@assert:` directives are checked by scenario
tests, not in the timed benchmark loop. A benchmark failure returns exit code **1**. Invalid flags or no
matching scenarios return **2**; a successful run or `--list` returns **0**.

Treat custom `.nflow` files as untrusted code: like `nflow run`, benchmarking a
custom flow may cause arbitrary effects allowed by its actions. Use only flows
you trust and account for any external side effects they may perform. Scenario
file discovery rejects symlinks. Custom flows use the capabilities linked into
the current binary; `self bench` does not build a new `@require` harness. In JSON
mode, scenario stdout is redirected to stderr to keep the report parseable.

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
automatically. This command and its `--version` / `-v` aliases stay offline.

```sh
nflow version check
```

Explicitly queries the [latest stable GitHub release](https://github.com/nexssp/flow/releases/latest)
using the public Releases API (10-second timeout). Prints current/latest versions
and, only when a newer stable release is found, an exact manual update command:

```sh
go install github.com/nexssp/flow/cmd/nflow@v0.39.0
```

The version above is illustrative; the command pins the tag returned by GitHub.
It requires Go and installs into `GOBIN` or `GOPATH/bin`; check that this is the
`nflow` on your `PATH`. Prebuilt-binary users can instead download and verify the
matching [release archive](releases.md#download-and-verify). Nothing is installed
or updated automatically. GitHub's latest published stable release is the source,
not unreleased tags, prereleases, or the Go proxy's potentially different `@latest`.

Development, pseudo-version, git-describe, and unknown builds are identified
without claiming an update is available. A network/API failure (including rate
limits), absent release, or invalid release response prints `Latest: unavailable`
and explains that availability could not be determined; exit status is **1**.
A completed lookup returns **0**, whether or not an update is available or the
current build can be compared. Invalid usage returns **2**.

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

For external-module `nflow run` calls, the CLI may use a thin runner when
the command is exactly `nflow run FILE [JSON_OBJECT] [--json]`, stdin is a
terminal, and stdout is redirected. It does not select this fast path for
piped stdin, terminal stdout, other subcommands, or additional flags such
as `--assert`, `--info`, `--journal-dir`, `--execution-id`, `--approval`,
or verbosity options; those use the full CLI harness. This conservative
stdin check intentionally sends even an empty pipe to the CLI harness so
input is never silently ignored. If a thin runner detects unsupported
conditions, it exits with the fallback sentinel and the CLI retries through
the full harness.
Even `--json` with terminal stdout stays on the full harness; this is a
deliberately conservative optimization boundary, not a difference in JSON
semantics.

The fast path uses the same execution-failure heading and positioned source
snippet as the CLI. Verbosity flags are outside the fast-path boundary and use
the full CLI harness; as with a normal CLI run, `-v` or higher enables the
execution trace.

To repeat the thin-versus-CLI harness measurements, run `task bench:harness`.
The cold-build benchmark uses the same tiny `@require ./helpers` flow for
both modes, gives each mode a fresh runner cache and a separate empty Go
build cache, and reports binary size. The warm-run benchmark starts each
already-built harness 50 times and reports per-process latency. The module
download cache is shared, so first-time network downloads are not part of
the cold-build comparison. `NFLOW_HARNESS_TIMINGS=1` reports preparation
and resolution separately from generated-module setup and Go build; the
reported phase sum excludes cache lookup/locking and process execution.

Previously measured on the `nexssflow` adapter (with module downloads
excluded): a cold Go cache and fresh runner cache took 62.914 s for the CLI
harness and 29.946 s for the thin harness; with a matched warm Go cache and
fresh runner cache, builds took 7.401 s and 5.206 s; warm run medians were
7.707 ms and 5.966 ms, respectively. The parent CLI was prebuilt for those
measurements. The benchmark command reports current binary sizes for its
own repeatable helper fixture.

On the current helper fixture, one isolated cold build measured 28.409 s
for thin (15,953,262-byte binary) and 73.234 s for CLI (43,476,371-byte
binary). Across 50 warm launches, the mean process startup was 7.049 ms for
thin and 9.074 ms for CLI. Treat these as machine- and fixture-specific
baselines; compare modes from the same run rather than comparing absolute
times across hosts.

Bundle options declared in `{ ... }` are validated by the bundle. An
unrecognized key is a compile error:

```
@require ./helpers { preifx: "DEMO" }
  error: @require ./helpers: unknown option "preifx" (accepted: prefix)
```

See [extensions.md](extensions.md) for the bundle contract.

---

## Manage requirements

The `require` commands inspect and edit direct `@require` selectors in the named source file:

```sh
nflow require check flows/client.nflow
nflow require update flows/client.nflow --package 'github.com/acme/*'
nflow require pin flows/client.nflow --package github.com/acme/client
nflow pin flows/client.nflow  # alias for require pin
nflow require reset flows/client.nflow
```

`check` is read-only and reports each selected remote requirement's current canonical selector and the latest available version. `update` queries latest for selected direct remote requirements. `pin` resolves and canonicalizes each existing selector (branch, tag, hash, or `latest`); a selector omitted from a remote requirement resolves to `latest` only when no local/workspace provider supplies it. Local, workspace, and built-in requirements are skipped. `--package` accepts an exact import path or a glob; without it, every direct requirement in the named file is selected. Included files are never edited. See [Managing requirements](requirements.md) for backup behavior and matching semantics.

### Compiler-version guard

Declare `@nflow_version ">=0.38.0"` in a flow to require a compatible compiler. This is a compatibility range, not an engine selector. The generic `nflow` CLI checks file-based commands before compiling; if its installed release is outside the range, it selects the newest official stable release satisfying the range, downloads the platform archive, verifies its published SHA-256 checksum, and caches the verified core. If the installed release already satisfies the range, no download occurs. A development or unknown build also resolves a declared constraint to a matching stable release because it cannot establish compatibility itself. For equality-only version constraints, a verified cache hit is hash-checked and reused without contacting the release API; range constraints still query online for the newest compatible release. Offline resolution uses only a previously verified compatible cache. With no source constraint, development/unknown builds keep running unchanged; an exact `nflow.lock` or `--nflow-version` selection is honored whether the launcher is a development or release build.

For source-file execution (`nflow file.nflow` or `nflow run file.nflow`), the launcher reports an actual core switch on `stderr` before execution. A switch reports both the current and selected release; a development launcher selecting a released core reports `NFlow current dev -> selected v<version>`. When the current core is selected, the `using NFlow <version>` notice is shown only with `LOG_LEVEL=debug`; actual switches remain visible at every log level. Notices include `@nflow_version`, `nflow.lock`, or `--nflow-version` details when they determine selection. They do not alter workflow output on `stdout`; formatter, lint, and info commands do not print runtime notices.

For reproducible projects, explicitly write an exact release pin:

```sh
nflow toolchain pin v0.44.2
```

This writes only `nflow.lock` in the current directory, containing the exact tag `v0.44.2`; it does not edit `.nflow` source. Running `nflow toolchain pin` without a version pins the current stable release. A one-off selector also works without changing the lock:

```sh
nflow --nflow-version v0.44.2 run flows/client.nflow
```

When a lock or explicit selector is present, the exact official release is used and must still satisfy every declared `@nflow_version` range. The launcher passes command arguments directly (without a shell), preserves standard streams and exit status, and marks the child process to prevent repeated resolution. If offline, it uses only a previously verified compatible cache; otherwise it reports that no verified cache is available. `nflow lsp` and `nflow fmt --stdin` have no source path at startup, so use an explicit selector or `nflow.lock`; editor clients should run them from the project directory.

The previous directive spelling is no longer supported. The constraint grammar remains whitespace-separated comparisons (`=`, `==`, `>=`, `<=`, `>`, `<`) with AND semantics; it does not add OR, wildcard, caret, or tilde ranges.
