# Flow extensions

Every extension is one self-contained package under `extensions/<name>/`
that plugs into the compiler through `compiler.Bundle`. Core knows only the
bundle contract; the feature lives entirely here.

## Anatomy

    extensions/loop/
    ├── library.go          # Package comment, ID, init(), Bundle()
    ├── keyword.go          # Feature logic (parser, action, operator, ...)
    ├── nflows/             # Required executable conformance suite
    │   ├── basic.nflow
    │   ├── recovery.nflow
    │   ├── load.nflow
    │   └── load--settings.yml
    ├── keyword_test.go     # Tests for this package only
    └── library_test.go     # Wiring: Bundle() returns what it claims

`library.go` never contains feature logic. Feature files are named after
what they provide: `keyword.go`, `directive.go`, `modifiers.go`,
`actions.go`, `primary.go`.

## The Bundle contract

    const ID = "loop"

    func init() {
        compiler.Register(ID, Bundle)
    }

    func Bundle(_ map[string]string) compiler.Bundle {
        return compiler.Bundle{
            ID:        ID,
            Libraries: []action.Library{{Name: ID}},
            Primaries: []compiler.PrimaryExtension{loopKeyword{}},
            Fixtures:  fixturesFS,  // required for every extension
        }
    }

ID is a short label — `"loop"`, `"macros"`, `"fs"` — never an import
path. It survives forks, module moves, and vendor dirs. Two extensions
with the same ID panic at `init()`.

## Fixtures vs SelfTest

Every extension must embed at least one runnable `.nflow` fixture directly
in `nflows/`. Keep behavior-specific names in the filenames rather than
creating `happy/`, `errors/`, or `edge/` subdirectories. The complete suite
runs through `nflow self test`; no extension should be left without a passing
fixture.

A fixture is a real `.nflow` file the runner executes like any pipeline:

    @description "Loop loop(...) until(...)"
    @assert: result.n == 3
    { n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )

The `@description` names the feature in `nflow self test`. Companion files
also live directly in `nflows/`: name them `<fixture-stem>--<relative-path>`,
using `--` in place of each directory separator. For example, `read.nflow`,
`read--input.json`, and `read--schemas--settings.yml` expose `input.json` and
`schemas/settings.yml` to that fixture. A child flow such as
`read--child.nflow` is treated as a companion, not a separate self-test. This
keeps file-reading scenarios local and deterministic without nested folders.
The fixture grows with the DSL — no inline Go DSL string to update when the
feature evolves.

Keep behavior in the `.nflow` fixture whenever it can be expressed and
asserted there. Use inline `SelfTest()` only when a deterministic NFlow
cannot exercise the behavior, such as an unavoidable live external
resource; do not keep duplicate inline DSL cases alongside fixtures.

## Integrated specifications

Real-world scenarios that combine several extensions belong in the root
[`spec/`](../spec/README.md) tree, not in an extension's conformance
suite. A scenario may own a custom `bundle/library.go` and use
`@require ./bundle`; keep it deterministic, assert meaningful outcomes,
and use it to demonstrate composition rather than repeat a single
extension fixture. `task specs`, `task test`, and `task ci` execute these
integrated scenarios.

## Tests

Extension tests verify **this package's code only**:

- Parser output — does `loop(...)` produce the expected AST node.
- Translation — does `:timeout=5s` set `Meta.Timeout = 5s`.
- Wiring — does `Bundle()` return the promised directives, primaries,
  libraries, and fixtures.

Never test kernel runtime here (retries, cache hits, concurrency,
context semantics). That is `kernel/*`'s job; its suite already covers
it. Never use `time.Sleep` in these tests.

Three rules that prevent 90% of the flaky tests in this package:

- **Reading from `map[string]any`:** assert the type first
  (`v, ok := m[key].(string)`) before passing to `ktest.RequireEqual`.
  Generic inference cannot resolve `any` against a concrete want.
- **Absolute paths:** use `t.TempDir()`, not a hardcoded `/abs/path`.
  `filepath.Abs("/x")` returns `C:\x` on Windows; a test comparing
  against `/x` will fail.
- **Stream operators are not predicates.** `action.StreamOp` returns an
  `iter.Seq2`, not `func(T) bool`. Test through a `collectX` helper that
  iterates to exhaustion.

## Errors

One rule, no exceptions:

- **Compile-time with a position:** `compiler.SourceError(pos, ...)`.
- **Runtime, reaches the user:** `xerr.<Kind>(...)`.
- **Internal wiring (bug, not user input):** `xerr.Internal(...)`.
- **`fmt.Errorf`** is allowed only inside helpers that are immediately
  wrapped by `SourceError` in the same file (tag parsers, field
  parsers). Anywhere else it means the error escapes unclassified.

Before commit: `rg 'fmt\.Errorf' flow/extensions/` — every hit must be
justified by the rule above.

## External repositories

Any repo can become a Flow extension by adding a `nexssflow/` package:

    my-repo/
    ├── go.mod                  # module github.com/nexssp/my-repo
    └── nexssflow/
        └── library.go      # exposes Bundle(opts) compiler.Bundle

Then `@require github.com/nexssp/my-repo v1.2.3` selects package
`github.com/nexssp/my-repo/nexssflow`. Go determines whether the package
comes from the root module or an independently versioned nested module;
the requested version applies to the module that provides it. An explicit
variant such as
`@require github.com/nexssp/my-repo/nexssflow_v2 v1.4.0` selects that Go
package path, and Go determines its provider. Flow does not infer or
truncate module paths from repository URL shape. Remote requirements are
version-pinned; unversioned subpackages are only for the current module or
Go workspace, not an implicit `latest` request.

The shim can be a thin wrapper over `external.ExecAction`, a `sandbox`
engine, or a direct Go import if the repo is already in Go.

## Rules

- Package comment: `// Package <name> ships <what>.` + `Typical use:` block.
- No `const ImportPath`. No hardcoded module URLs anywhere.
- No silent anything — collisions panic; missing contracts fail loudly.
- Files split by responsibility, not by line count.
