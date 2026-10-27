# `extensions/lang` — inline polyglot execution

Add `@require lang` to a `.nflow` file and you can run code in any language
the bundle ships directly from the pipeline:

```
@require lang

{ "x": 2, "y": 3 } -> py"import json, sys; v=json.load(sys.stdin); print(json.dumps({'value': v['x'] + v['y']}))"

{ "x": 2, "y": 3 } -> py {
    import sys, json
    data = json.load(sys.stdin)
    print(json.dumps({"value": data["x"] + data["y"]}))
}
```

Each tag (`py`, `rs`, `go`, ...) is a **keyword** in the nflow grammar —
same dispatch mechanism as `loop` and `until`. The tag compiles to a
single `LangBlock` AST node. The runtime materializes the source to a
temp file, resolves the interpreter, and delegates execution to
`extensions/external`. **No new subprocess engine lives in this bundle.**

Existing scripts can be called without writing `external.exec` options. The
path is resolved beside the `.nflow` file; it is passed directly to the selected
interpreter, not through a shell:

```nflow
@require lang

{ "samples": [1.2, 1.8, 2.1] } -> py.file "./analysis.py"
```

The script reads the incoming JSON from stdin and writes its result as JSON to
stdout, just like an inline block. Imports and the original script filename are
preserved. `@include` remains for importing `.nflow` modules; use `py.file` for
Python programs. Other runner aliases also provide a `.file` form, such as
`js.file "./report.js"` or `rs.file "./tool.rs"`.

Missing files remain an error unless that call opts in with `:autocreate`:

```nflow
{ "orders": [] } -> go.file "./normalize.go" :autocreate
```

The runner creates the file beside the `.nflow` source from that language's
`file_template`. It uses exclusive creation, so an existing file is never
overwritten. The starter reads one JSON value from stdin and writes one JSON
value to stdout; edit the generated source to add processing. If a runner has no
template, autocreate returns an error instead of guessing source code.

The `@` prefix is not used for tags — the nflow grammar already uses `@`
for atom modifiers (`@config`, `@prompt`). Keywords are the established
idiom for in-expression dispatch.

## Data-driven runners (no Go code per language)

Every language is defined by a **runner file** under `runners/*.nflow`, not
by Go code. The bundle embeds these files via `//go:embed` and loads them
at `init()` through `LoadRunners()`. This mirrors the `nexssp/ai` pattern
(`SourceModules` exposed through `compiler.Bundle`).

Adding a language = adding one `runners/<id>.nflow` file. Zero Go changes.

### Runner file format

```
# runners/python.nflow (strict JSON; `_comment` is an optional note)
{
    "_comment": "Python (default).",
    "id": "python",
    "ext": ".py",
    "aliases": ["py", "python"],
    "find": ["python3", "python", "py -3"],
    "stdin": "json",
    "stdout": "json",
    "file_template": "import json\\nimport sys\\n\\ninput_value = json.load(sys.stdin)\\noutput_value = input_value  # TODO: transform input\\njson.dump(output_value, sys.stdout, separators=(\\\",\\\", \\\":\\\"))\\nsys.stdout.write(\\\"\\\\n\\\")\\n",
    "install": {
        "windows": ["winget", "install", "Python.Python.3.12"],
        "darwin": ["brew", "install", "python@3.12"]
    }
}
```

Versioned variants are separate files:

```
# runners/python3.nflow
{
    "id": "python",
    "version": "3",
    "ext": ".py",
    "aliases": ["py3", "python3"],
    "find": ["python3", "py -3"],
    "stdin": "json",
    "stdout": "json"
}
```

### Fields

| Field       | Required | Description                                              |
| ----------- | -------- | ------------------------------------------------------- |
| `ext`       | yes      | File extension for materialized source, incl. the dot  |
| `aliases`   | no       | Keywords (`py`, `python`) that resolve to this spec     |
| `find`      | yes      | Candidate command lines tried in order; first on PATH wins |
| `stdin`     | no       | `json` (default) or `text`/`raw`                       |
| `stdout`    | no       | `json` (default) or `text`/`raw`                       |
| `file_template` | no   | Starter source for `.file :autocreate`; requires JSON stdin and stdout |
| `install`   | no       | Per-OS auto-install recipe; omit an OS entry to skip   |

Runner files are strict JSON: keys and string values must be quoted, and
trailing commas are not accepted. Use the optional `_comment` field for notes.
The same optional `file_template` field is accepted by `@lang.define` and
`@lang.runner`, so new languages can participate without Go-side template code.

## Tags and aliases

Short and long forms both work. Aliases are derived from the runner files
at `init()` — the table is built, not hand-maintained.

| Tag             | Runner file              | Version |
| --------------- | ------------------------ | ------- |
| `py`            | `runners/python.nflow`   | default |
| `python`        | `runners/python.nflow`   | default |
| `py2`           | `runners/python2.nflow`  | `2`     |
| `python2`       | `runners/python2.nflow`  | `2`     |
| `py3`           | `runners/python3.nflow`  | `3`     |
| `python3`       | `runners/python3.nflow`  | `3`     |
| `rs`            | `runners/rust.nflow`     | default |
| `rust`          | `runners/rust.nflow`     | default |
| `go`            | `runners/go.nflow`       | default |
| `golang`        | `runners/go.nflow`       | default |
| `ts`            | `runners/typescript.nflow` | default |
| `typescript`    | `runners/typescript.nflow` | default |
| `js`            | `runners/javascript.nflow` | default |
| `node`          | `runners/javascript.nflow` | default |
| `sh`            | `runners/shell.nflow`    | default |
| `bash`          | `runners/shell.nflow`    | default |

## Inline Python dependencies

Python scripts can declare dependencies using [PEP 723](https://peps.python.org/pep-0723/). If the source contains the `# /// script` marker, the runner uses `uv run` so uv can read the metadata and manage the environment:

```nflow
@require lang
py {
# /// script
# dependencies = ["httpx"]
# ///
import json, httpx
print(json.dumps(httpx.__version__))
}
```

Preflight checks that `uv` is on `PATH` and reports it if missing; it does not install uv. Without the marker, Python uses the ordinary interpreter selected by the runner profile.

## Preflight — the pre-run gate

Before any `LangBlock` executes, the compiler checks every interpreter used by
the flow. It first searches `PATH`; if one is missing, it runs that runner's
per-OS install recipe and resolves it again. If installation is unavailable or
fails, the preflight groups the remaining missing interpreters into one report
and **the flow does not run.**

```
lang preflight: missing interpreters — install before running:
  lang.python@3 (keyword py3)
  lang.rust (keyword rs)
```

The normal `@require lang` compile preflight uses the runner's installer
automatically. A direct call to `Preflight.Run` remains find-only unless the
caller supplies an installer explicitly:

```go
pf := lang.Preflight{Installer: myInstaller}
report := pf.Run(blocks)
// report.OK() is true if every interpreter was either present or installed
```

Install recipes are data-driven in each runner profile. Python's default and
`py3` profiles provide Windows, macOS, and Linux recipes. Installation requires
the relevant package manager and its normal permissions; on failure,
`NotInstalledError` carries the cause. PEP 723 scripts still require `uv`, which
is checked separately and is not automatically installed.

## Stream contract

By default, the incoming stream value is JSON-encoded to stdin and stdout
is JSON-decoded back into the next atom's input. `sh` and any language
declared with `"stdin": "text"` use raw strings instead.

A language with no output produces `nil` in the stream — not an error.
Empty stdin is also `nil`, not `null`.

## `@lang.define` — register a language inline

Same data model as a runner file, declared in-flow:

```
@lang.define "inlinepy" {
    "id": "inlinepy",
    "ext": ".py",
    "aliases": ["inlinepy"],
    "find": ["python3"],
    "stdin": "json",
    "stdout": "text"
}
{ "value": "x" } -> inlinepy"import sys; sys.stdout.write('ok')"
```

Definitions use strict JSON and their aliases are made available while that
flow is compiled. They remain in the process-wide language registry after
compilation, so long-running hosts must not recompile definitions with the same
ID or alias; use unique definitions or register runners once in host code.

## Trust model

`py"..."` is arbitrary code execution. So was `external.shell` before
this bundle existed — the threat model does not change, only the
ergonomics of doing the dangerous thing get better. A sandbox is **not**
provided.

## Why this bundle is opt-in

The bundle is linked and registered for the CLI's `@require` loader, but is
not mounted in `native.Bundles()`. Only flows that add `@require lang` receive
its directives, keywords, and preflight checks.

## Package layout

The Go sources implement the bundle, parser primary, runner registry,
preflight validator, temp-file lifecycle, and adapter to `extensions/external`.
Runner profiles live in `runners/`; executable conformance flows live in
`nflows/`. The CLI keeps this bundle out of its default mount set, and the
repository self-test suite runs its fixtures while skipping only when a
required interpreter is absent.
