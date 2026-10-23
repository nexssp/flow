# NFlow environment

```sh
nflow env
```

This offline, read-only command shows only two categories:

- **NFlow configuration:** `NFLOW_GO_VERBOSE`, `NFLOW_GO_TIMEOUT`,
  `NFLOW_HARNESS_CACHE`, `NFLOW_HARNESS`, `LOG_LEVEL`, `LOG_FORMAT`.
- **Standard variables also read directly by NFlow:** `NO_COLOR`, `GOBIN`,
  `GOPATH`, `GOWORK`, `TEMP`, `TMP`.

Each entry includes its current **set/unset** state, a quoted value, its purpose
and defaults. An explicitly empty value appears as `set ""`, not `unset`.
Quoting prevents embedded newlines and terminal escapes from altering the report.
The inventory and descriptions live together in `cli/env.go`.

The report excludes Go toolchain settings merely inherited by subprocesses,
HTTP proxy settings, indirect standard-library directory/tool-lookup defaults,
and arbitrary application keys selected by `runtime.env`. No environment dump,
arbitrary key lookup, flow execution or `go env` subprocess is performed.
The values describe the current process, not settings saved with `go env -w`.
Review machine-specific paths before sharing the output.

`nflow env --help`, `nflow help env`, and shell completion are supported.
Unexpected arguments return usage and exit status 2; a report or help returns 0.

## Terminal appearance

`nflow env` and the human-readable `nflow self bench` report share the self-test
palette and visual hierarchy: magenta banner rules, bold headings, cyan section
markers, green configured/success states, red failures, and dim secondary details.
The existing self-test implementation is unchanged.

Colors are enabled only when output is a terminal. Redirection and pipes produce
plain text, as does a non-empty `NO_COLOR` value. Empty `NO_COLOR` does not disable
colors. `nflow self bench --json` and `--list --json` remain color-free JSON with
unchanged fields. Benchmark measurements and exit codes are unchanged.

## Quiet harness dependency resolution

Routine successful Go output, including `go: added`, `upgraded`, `downgraded`,
`removed`, and download chatter, is hidden. Go warnings remain visible as concise
one-line messages. Retraction warnings identify the dependency, version, and
reason, and appear at most once per dependency/version per NFlow invocation,
even when several resolver/build steps report the same warning. Deduplication
resets for the next invocation; it does not fix or change retracted dependencies.

Failures retain captured diagnostics in the returned error. Set
`NFLOW_GO_VERBOSE=1` to stream the original Go diagnostics without filtering or
deduplication; this also disables the progress spinner. The existing bounded
error tail is not an unlimited transcript. CLI/status output and workflow stdout
are unchanged.

Harness Go commands override `GOWORK=off` and `GIT_TERMINAL_PROMPT=0`.
NFlow workspace discovery reads the original `GOWORK` value before building a
harness. `self up` inherits the environment unchanged.
