# Flow extensions

Every extension is a self-contained package that plugs into the compiler
through `core.Bundle`. Core knows only the bundle contract; the feature logic
lives entirely inside the extension.

## Anatomy (In-Tree Native Extensions)

    flow/extensions/loop/
    ├── library.go          # Package comment, ID, init(), Bundle()
    ├── keyword.go          # Feature logic (parser, action, operator, ...)
    ├── nflows/             # Optional DSL fixtures, auto-discovered
    │   └── basic.nflow
    ├── keyword_test.go     # Tests for this package only
    └── library_test.go     # Wiring: Bundle() returns what it claims

`library.go` never contains feature logic. Feature files are named after
what they provide: `keyword.go`, `directive.go`, `modifiers.go`,
`actions.go`, `primary.go`.

## The Bundle Contract

```go
const ID = "loop"

func init() {
    core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
    return core.Bundle{
        ID:        ID,
        Libraries: []action.Library{{Name: ID}},
        Primaries: []core.PrimaryExtension{loopKeyword{}},
        Fixtures:  fixturesFS,  // optional embed.FS
    }
}
```

- `ID` is a short label (`"loop"`, `"sandbox"`, `"fs"`) — never an import path. It survives forks, module moves, and vendor directories.
- Two extensions with the same ID panic at `init()`.
- `opts` receives options passed in the `@require` block parsed via `core.Decode[T](opts)`.

---

## External Repositories (Any Language)

Flow is language-agnostic. Any repository (Python, Node.js, Rust, C++, or Go) can expose capabilities to Nexss Flow by adding an extension bundle directory.

### 1. Default Convention (`nexssflow/`)

By default, an extension lives in a `./nexssflow` subdirectory:

    my-project/
    ├── (any language: Python scripts, Node packages, Rust crates, etc.)
    └── nexssflow/
        └── library.go      # Exposes Bundle(opts) core.Bundle

In your `.nflow` workflow, omit the subfolder:

```nflow
@require github.com/nexssp/my-project
# Resolves automatically to github.com/nexssp/my-project/nexssflow
```

### 2. Multi-Bundle Variants (`nexssflow_<variant>/`)

A single repository can provide multiple bundle flavors (e.g. dev mocks, production backends, specialized profiles) by naming explicit directories:

    my-project/
    ├── nexssflow/          # Default profile
    │   └── library.go
    ├── nexssflow_dev/      # Development mock profile
    │   └── library.go
    └── nexssflow_prod/     # Production hardened profile
        └── library.go

In `.nflow`, reference the specific target:

```nflow
# Uses the custom variant directly without appending /nexssflow
@require github.com/nexssp/my-project/nexssflow_dev
```

### 3. Local Loose Packages (Zero Config / Scratch)

For rapid local iteration, a local folder with Go files does not need its own `go.mod`:

```nflow
@require ./localtest
```

- If `./localtest/nexssflow` exists, Flow uses it.
- If `./localtest` contains the bundle `.go` files directly, Flow uses `./localtest`.
- If no ancestor `go.mod` exists, Flow vendors the directory into the temporary execution harness as an internal subpackage. **Zero `go.mod` files are written to the user workspace.**

---

## Resolution Rules for `@require`

1. **Remote Module Root (`@require github.com/org/repo`):**
   Automatically appends `/nexssflow` by convention.
2. **Explicit Subpackage (`@require github.com/org/repo/nexssflow_dev`):**
   Preserved as-is. Flow does not append `/nexssflow` if an explicit subpackage is given.
3. **Local Path (`@require ./sub/dir`):**
   Walks upward for `go.work` or `go.mod`. If found, resolves natively as a subpackage. If no `go.mod` exists, auto-bundles as a loose package.
4. **Missing Bundle:**
   If no bundle is found, Flow halts with an actionable error directing the developer to scaffold it with `nflow init --bundle`.

---

## Scaffolding Extensions (`nflow init`)

To quickly generate an extension bundle inside any existing repository:

```powershell
# Scaffolds ./nexssflow/library.go in the current directory
nflow init --bundle

# Scaffolds a custom variant ./nexssflow_dev/library.go
nflow init --bundle nexssflow_dev
```

To create a complete starter project (workflow, local helper bundle, assertions, and README):

```powershell
# Scaffolds a full Flow project in ./my-pipeline
nflow init my-pipeline
```

---

## Fixtures vs SelfTest

Prefer `nflows/*.nflow` fixtures over inline `SelfTest()`. A fixture is a real `.nflow` file the runner executes like any pipeline:

```nflow
@description "Loop loop(...) until(...)"
@assert: result.n == 3
{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
```

The `@description` names the feature in `nflow self test`. The file grows with the DSL — no Go code to change when the feature evolves.

Use inline `SelfTest()` only when the feature cannot be expressed in pure DSL:
- Temporary files needed outside the workspace (`Files:` map).
- Subprocesses requiring mocked systems (`exec`, `http.request`).
- Runtime-only middleware without observable state assertions (`:cache=`, `:retry=`).

---

## Tests

Extension tests verify **this package's code only**:

- **Parser output:** does `loop(...)` produce the expected AST node.
- **Translation:** does `:timeout=5s` set `Meta.Timeout = 5s`.
- **Wiring:** does `Bundle()` return the promised directives, primaries, libraries, and fixtures.

Never test kernel runtime here (retries, cache hits, concurrency, context semantics). That is `kernel/*`'s job; its suite already covers it. Never use `time.Sleep` in these tests.

Three rules that prevent 90% of flaky tests:

- **Reading from `map[string]any`:** assert the type first (`v, ok := m[key].(string)`) before passing to `ktest.RequireEqual`. Generic inference cannot resolve `any` against a concrete target.
- **Absolute paths:** use `t.TempDir()` and `t.Chdir()`, not hardcoded paths.
- **Stream operators are not predicates:** `action.StreamOp` returns an `iter.Seq2`, not `func(T) bool`. Test through a `collectX` helper that iterates to exhaustion.

---

## Errors

One rule, no exceptions:

- **Compile-time with a position:** `core.SourceError(pos, ...)`.
- **Runtime, reaches the user:** `xerr.<Kind>(...)`.
- **Internal wiring (bug, not user input):** `xerr.Internal(...)`.
- **`fmt.Errorf`** is allowed only inside helpers that are immediately wrapped by `SourceError` in the same file (tag parsers, field parsers). Anywhere else, the error escapes unclassified.

Before commit: `rg 'fmt\.Errorf' flow/extensions/` — every hit must be justified by the rule above.

---

## Rules

- Package comment: `// Package <name> ships <what>.` + `Typical use:` block.
- No `const ImportPath`. No hardcoded module URLs anywhere.
- No silent failures — collisions panic on boot; missing contracts fail loudly.
- Files split by responsibility, not by line count.
