# Flow extensions

Every extension is one self-contained Go package under
`extensions/<name>/` that plugs into the compiler through the
`compiler.Bundle` contract. Core knows only that contract; the feature
lives entirely in the extension.

This document is the reference for building and shipping a bundle.
The [CLI guide](cli.md) covers `@require` and harness resolution; this
file covers the contract your bundle must implement.

---

## 1. Anatomy

```
extensions/loop/
├── library.go            # Package comment, ID, init(), Bundle()
├── keyword.go            # Feature logic (parser, action, operator, ...)
├── selftest.go           # Optional inline SelfTest()
├── nflows/               # Required executable conformance suite
│   ├── basic.nflow
│   ├── recovery.nflow
│   ├── load.nflow
│   └── load--settings.yml
├── keyword_test.go       # Tests for this package only
└── library_test.go       # Wiring: Bundle() returns what it claims
```

`library.go` never contains feature logic. Feature files are named after
what they provide: `keyword.go`, `directive.go`, `modifiers.go`,
`actions.go`, `primary.go`, `library.go` (wiring), `selftest.go`.

The file split is by responsibility, not by line count.

---

## 2. The bundle contract

The smallest bundle that does something:

```go
// Package hello ships a single action that returns "hello".
package hello

import (
    "context"

    "github.com/nexssp/kernel/action"

    compiler "github.com/nexssp/flow/compiler"
)

const ID = "hello"

func init() {
    compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
    greeting := action.New("hello.greet", func(ctx context.Context, in any) (any, error) {
        return "hello", nil
    }).Description("Return a greeting").Build()

    return compiler.Bundle{
        ID:        ID,
        Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{greeting}}},
    }
}
```

That is a complete, working bundle. It registers one action,
`hello.greet`. To use it, put it in a module that a flow can `@require`.

### Bundle fields

```go
type Bundle struct {
    ID           string
    Alias        string          // set by @require ... as NAME; do not set in code
    Libraries    []action.Library
    Directives   []compiler.Directive
    Modifiers    []compiler.Modifier
    Operators    []compiler.Operator
    Primaries    []compiler.PrimaryExtension
    Materialize  compiler.Materializer
    MaterializePhase compiler.MaterializePhase
    OnPreprocess func(meta map[string]any) compiler.PreprocessContributions
    AtomAdvise   func(atom *compiler.Atom, builder *action.Builder[any, any]) error
    WrapPipeline func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error)
    Hooks        []action.AnyHook
    FileResolvers []compiler.FileResolverBinding

    ArgSchemas      map[string][]compiler.ArgFieldSpec
    AcceptedOptions []string
    Symbols         []compiler.SymbolNamespace

    SelfTest func() []compiler.SelfTestSection
    Fixtures fs.FS
    Shutdowns []compiler.ShutdownFunc
    Build compiler.BuildExtension
}
```

| Field | Purpose |
|---|---|
| `ID` | Short label. Never an import path. |
| `Alias` | Filled in by the harness when `@require ... as NAME` is used. Never set it yourself. |
| `Libraries` | Actions, stream sources, and stream operators this bundle publishes. |
| `Directives` | Compile-time `@name` handlers. |
| `Modifiers` | `:name` and `:name=value` annotations this bundle accepts. |
| `Operators` | Binary pipeline operators (rare; the syntax bundle uses this). |
| `Primaries` | Parser extensions that intercept a token or keyword. |
| `Materialize` | Mount additional actions from `meta` after preprocessing. |
| `MaterializePhase` | Selects `MaterializeDeclarations` (the zero value) or `MaterializeCompositions`; order is stable within each phase. |
| `OnPreprocess` | Contribute additional primaries or compile options after preprocessing. |
| `AtomAdvise` | Mutate an atom's builder before it runs. |
| `WrapPipeline` | Wrap the compiled program (catch errors, install hooks, publish context). |
| `Hooks` | Kernel action hooks mounted with the bundle's action libraries. |
| `FileResolvers` | Provider-scoped `xfile.Resolver` bindings. `runner.BuildConfig` composes them automatically and exposes the resolver through the runtime context. |
| `ArgSchemas` | Declare which action arguments are capability references. |
| `AcceptedOptions` | Whitelist keys accepted in `@require ... { ... }`. `nil` accepts anything. |
| `Symbols` | Global symbol namespaces contributed by the bundle. |
| `SelfTest` | Inline features run by `nflow self test`. |
| `Fixtures` | Embedded `.nflow` files run as features. |
| `Shutdowns` | Context-aware cleanup callbacks invoked by the runner host. |
| `Build` | Optional source-level contribution to standalone build planning. |

### `ID` rules

- Must be non-empty, non-whitespace, and unique in the process.
- Two bundles with the same ID panic at `init()`.
- Not an import path. Not a module URL. Survives forks, renames, and vendor dirs.

---

## 3. What a bundle may not do

Four hard rules. Each is enforced at mount time; none is a convention.

### 3.1 No reserved names

Native keywords are grammar. A bundle cannot register an action, source,
or operator named `const`, `noop`, `pick`, `wrap`, `fail`, `sleep`,
`env`, `uuid`, `json`, or `with`:

```
library "mybundle": action "const" is a reserved language name
```

The reserved list is in `compiler/reserved.go` and is not extensible.

### 3.2 No reserved modifiers

Kernel policy modifiers are owned by Kernel; the grammar modifier
`:profile` is owned by the compiler. A bundle that declares `:timeout`
from any package except `modifiers_core` panics at table construction:

```
core: modifier timeout is reserved for the Kernel policy provider
```

If your bundle needs a modifier, name it after your domain:
`:model`, `:budget_usd`, `:nats`, `:schema`.

### 3.3 No kernel action aliases

Kernel libraries may declare `Aliases` and `Overrides`. Flow rejects
both at mount time. The only alias mechanism Flow supports is the local
`@require ... as NAME` qualifier, which is per-compilation and does not
mutate the source library.

### 3.4 No unconditional primary installation

A primary that installs itself whenever the bundle is loaded collides
with the inherited top-level primary inside a sub-pipeline compile.
Install your primary from `OnPreprocess`, and only when the current
source actually uses it. See `extensions/macros/library.go` for the
pattern.

---

## 4. Actions

Actions are built through `kernel/action`. A bundle publishes actions by
listing them in a `Library`:

```go
import "github.com/nexssp/kernel/action"

Library: action.Library{
    Name:    ID,
    Actions: []action.AnyAction{
        fetchAction,
        transformAction,
        emitAction,
    },
}
```

Every action carries a **canonical, fully-qualified name**. `hello.greet`
is valid; `greet` is not. The name is `<namespace>.<verb>` and the
namespace is what a user searches for.

### 4.1 Request and response types

Typed actions declared with `action.New[Req, Res]` expose their input
and output shapes to `nflow show`, `nflow catalog`, and the compile-time
contract checker. Free-form actions receive `any` and expose no fields.

```go
type GreetReq struct {
    Name string `json:"name"`
}

greet := action.New("hello.greet", func(ctx context.Context, req GreetReq) (string, error) {
    return "hello, " + req.Name, nil
}).Build()
```

`nflow show hello.greet` lists `name` as an input field.

### 4.2 Stream sources and operators

```go
Library: action.Library{
    Name:    ID,
    Sources: []action.AnyStreamAction{itemsSource},
    Operators: []action.NamedOperator{
        filterOp,
        transformOp,
    },
}
```

A stream source is a lazy `iter.Seq2[Item, error]`. A stream operator
is a transform over a stream. Do not materialize the stream in a source;
the boundary is `collect`.

---

## 5. Directives

A directive is a compile-time `@name` handler that runs during
preprocessing.

```go
var Directive = compiler.Directive{
    Name:    "trace",
    Example: `@trace "starting fetch"`,
    Handler: func(ctx context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
        line := strings.TrimSpace(req.Lines[req.I])
        msg := strings.Trim(strings.TrimPrefix(line, "@trace"), `"' `)
        req.Out["trace"] = append(req.Out["trace"].([]string), msg)
        return compiler.DirectiveRes{Next: req.I + 1}, nil
    },
}
```

`DirectiveReq` gives you the source lines, the current index, the
`meta` map, the file path, and the base directory.

`DirectiveRes.Next` is the line to continue from. For a multi-line block,
read it with `compiler.ReadBlock`, which handles inline `{ ... }`, multi-line
`{ ... }`, and trailing-brace forms uniformly.

A directive that owns a **block** (like `@scope`) also sets
`BlankLines`: the 0-based line indices whose output should be empty
even though the source line had content. See `extensions/scope/directive.go`.

---

## 6. Modifiers

A modifier is a `:name` or `:name=value` annotation.

```go
compiler.Modifier{
    Name:    "budget_usd",
    Owner:   compiler.OwnerBundle,   // default; required for bundle-owned names
    Apply: func(b *action.Builder[any, any], raw string) error {
        limit, err := strconv.ParseFloat(raw, 64)
        if err != nil {
            return err
        }
        return b.Juliaptive(action.JuliaptiveConfig{Budget: limit})
    },
}
```

### 6.1 Use the typed constructors

Every modifier should parse its payload through a constructor from
`compiler/modifier.go`. `compiler.Duration`, `compiler.Int`, `compiler.Int32`,
`compiler.Int64`, `compiler.Float64`, `compiler.String`, `compiler.StringList`,
`compiler.Flag`. These set `ValueKind` automatically, so the linter can
catch `:budget=abc` statically.

```go
compiler.Duration("budget", func(b *action.Builder[any, any], d time.Duration) *action.Builder[any, any] {
    return b.Timeout(d)
})
```

### 6.2 Mark inheritable modifiers

A modifier that should propagate through `@scope` / `@profile` to nested
atoms must be marked `WithInheritable`:

```go
compiler.WithInheritable(compiler.Duration("timeout", (*action.Builder[any, any]).Timeout))
```

Metadata modifiers (`:tag`, `:status`, `:route`) are not inheritable.
`nflow explain` and the compiler both honor the distinction.

### 6.3 Mark unique modifiers

A modifier that should appear at most once on an atom is marked
`WithUnique`:

```go
compiler.WithUnique(compiler.Duration("timeout", (*action.Builder[any, any]).Timeout))
```

A second occurrence is a compile error:

```
modifier :timeout is not repeatable
```

---

## 7. Argument schemas

Some actions take arguments that name **another capability**. Declare
those fields in `ArgSchemas` and the compiler resolves and validates
them. `ArgCapabilityRef` and `ArgCapabilityRefList` fields are required
by default; set `Optional: true` only when the action intentionally accepts
the capability through runtime input instead. `Optional` does not change
the behavior of `ArgAny`, `ArgString`, `ArgInt`, or `ArgBool`.

```go
func Bundle(_ map[string]string) compiler.Bundle {
    return compiler.Bundle{
        ID:        ID,
        Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{distribute}}},
        ArgSchemas: map[string][]compiler.ArgFieldSpec{
            "distribute.map": {
                {Name: "action", Kind: compiler.ArgCapabilityRef},
            },
        },
    }
}
```

With that declaration:

```nflow
distribute.map @{ action: noop, items: [1, 2] }     # valid
distribute.map @{ action: noop, items: [1] } # valid
distribute.map @{ action: "noop", items: [1] }       # compile error
distribute.map @{ action: missing, items: [1] }      # compile error
```

An `ArgCapabilityRefList` field accepts a list of bare identifiers:

```go
{Name: "members", Kind: compiler.ArgCapabilityRefList},
```

```nflow
dispatch.run @{ members: [noop, const], payload: .data }
```

A field **not** declared in `ArgSchemas` is a plain string. A bare
identifier in an undeclared field stays a string; the compiler does not
guess.

Capability references are compile-time bindings. They must appear in the
action's `@{ ... }` block; a value in the pipeline input cannot satisfy a
required capability field. For example, `dispatch.run` requires its
`members` field in `@{}` and reports the source file and line if it is
missing. `runtime.call.name` is optional because that action deliberately
resolves a name supplied in its runtime input.

---

## 8. Options in `@require`

A flow can pass options to your bundle:

```nflow
@require github.com/example/fancy v1.2.3 { endpoint: "https://api.example", retries: "3" }
```

If `AcceptedOptions` is `nil`, your bundle accepts any key. If it is a
list, unknown keys are rejected:

```go
func Bundle(opts map[string]string) compiler.Bundle {
    return compiler.Bundle{
        ID:              ID,
        Libraries:       []action.Library{{Name: ID}},
        AcceptedOptions: []string{"endpoint", "retries"},
    }
}
```

```
@require github.com/example/fancy v1.2.3 { endpont: "..." }
  error: @require github.com/example/fancy: unknown option "endpont"
         (accepted: [endpoint, retries])
```

For typed options, use `compiler.Decode[T]`:

```go
type Options struct {
    Endpoint string        `nflow:"endpoint" default:"https://api.example"`
    Retries  int           `nflow:"retries"  default:"3"`
    Timeout  time.Duration `nflow:"timeout"`
}

func Bundle(raw map[string]string) compiler.Bundle {
    opts, err := compiler.Decode[Options](raw)
    if err != nil {
        panic(err)
    }
    // ...
}
```

`Decode` rejects unknown keys and unsupported field types; it is the
recommended shape for options with more than two fields.

---

## 9. Fixtures vs SelfTest

Each feature should have an executable fixture. Fixtures are executable
conformance cases, not snippets: `nflow self test` runs the fixtures contributed
by `native.SelftestBundles()` through the real compiler and runtime. Give each
fixture an `@description` and assert meaningful behavior. Keep every
`.nflow` directly in `nflows/`; use descriptive filenames instead of
`happy/`, `errors/`, or `edge/` subdirectories. For example:

```
extensions/loop/nflows/basic_loop.nflow
```

```nflow
@description "Loop until the counter reaches three"
@assert: result.n == 3
{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
```

Fixtures are discovered by walking the bundle's embedded `Fixtures` FS.
Companion inputs live directly in `nflows/` and use the name
`<fixture-stem>--<relative-path>`, with `--` standing for a directory
separator. For example, `read--input.json` is staged as `input.json`, while
`read--schemas--settings.yml` is staged as `schemas/settings.yml`. Companion
flows use the same convention and are staged for their parent rather than
run as standalone fixtures.

The `embed` extension requires an explicit host filesystem root, so its
[`host_root.nflow`](../extensions/embed/nflows/host_root.nflow) fixture is
embedded and executed by a dedicated Go test rather than the native CLI
self-test. Run `go test ./extensions/embed -run '^TestHostRootNFlowFixture$' -count=1`.
The shared `files` package is not a DSL bundle.


### Extension fixtures vs integrated specs

Use `extensions/<name>/nflows/` to prove that one extension's syntax and
behavior work. Use the root [`spec/`](../spec/README.md) tree when the
value is in a practical, multi-extension workflow. An integrated spec
may own a local `bundle/library.go` and require it with `@require ./bundle`;
keep these scenarios deterministic and assert their observable contract.
Tutorial-first walkthroughs live in the central
[examples repository](https://github.com/nexssp/examples/tree/main/flow).
`task self` runs the native self-test fixtures; `task lint:nflow` statically
checks the integrated sources without executing them. `task specs` executes
the integrated scenarios and their assertions. `task test` runs Go tests,
including the extension conformance gate and the host-root embed fixture,
but not the integrated scenarios. `task ci` includes self-tests, executed
specs, lint, and Go tests.

Compiler-only lexer, parser, AST, and compile-contract cases belong in
`compiler/*_test.go`; executable `.nflow` flows exercise registered
extensions and should not be collected into a compiler-owned bundle.

---

## 10. Tests

A bundle's tests verify **that bundle's code only**.

| Test scope | Where it belongs |
|---|---|
| Parser output — does `loop(...)` produce a `LoopExpr`? | `extensions/loop/keyword_test.go` |
| Wiring — does `Bundle()` return the promised directives and primaries? | `extensions/loop/library_test.go` |
| Modifier translation — does `:timeout=5s` set `Meta.Timeout`? | `extensions/modifiers_core/modifiers_test.go` |
| End-to-end DSL behavior — does a real flow produce the right output? | `extensions/<name>/nflows/*.nflow` or `*_test.go` using `runner.Execute` |

Never test Kernel runtime semantics here (retries, cache hits,
concurrency, context cancellation). Kernel's own suite covers those.
If your bundle's only behavior is "we call Kernel middleware X", the
test belongs in Kernel, not here.

### 10.1 Rules for stable extension tests

Three rules that prevent the flakiness this codebase has already
eliminated:

**Assert the type before comparing.** `map[string]any` values read from
the runner are `any`; the generic resolver in `ktest.RequireEqual`
cannot infer a concrete target from `any`.

```go
value, ok := m["count"].(int)
ktest.RequireCondition(t, ok, "count is %T, want int", m["count"])
ktest.RequireEqual(t, value, 3)
```

**Never hardcode absolute paths.** Use `t.TempDir()` and `t.Chdir`.
`filepath.Abs("/x")` on Windows is `C:\x`; a test comparing against
`/x` fails on the wrong OS.

**Stream operators are not predicates.** An `action.StreamOp` returns
`iter.Seq2`, not `func(T) bool`. Test through a `collectX` helper that
iterates to exhaustion.

**No `time.Sleep` for synchronization.** Use `xtest.Eventually`,
`xtest.Gate`, or a channel. Sleep-based tests pass on fast machines and
fail on CI.

### 10.2 Reading `map[string]any` from the runner

`runner.Execute` returns `Execution{Output any}`. The output is
whatever the last atom produced, unchanged. If your fixture asserts
`result.foo`, `result` is the output as a map:

```go
ex, err := runner.Execute(ctx, cfg, src, "test.nflow", nil)
ktest.RequireNoError(t, err)

m, ok := ex.Output.(map[string]any)
ktest.RequireCondition(t, ok, "output is %T", ex.Output)
ktest.RequireEqual(t, m["status"], "ok")
```

---

## 11. Errors

Three error classes, one rule each.

| Class | Use |
|---|---|
| Compile-time with a source position | `compiler.SourceError(pos, ...)` |
| Runtime, reaches the user | `xerr.<Kind>(...)` |
| Internal wiring bug | `xerr.Internal(...)` |

`fmt.Errorf` is allowed only inside helpers that are immediately wrapped
by `SourceError` in the same file (tag parsers, field parsers). Anywhere
else, an error escaping unclassified is a bug.

Before commit:

```sh
rg 'fmt\.Errorf' extensions/<your-bundle>/
```

Every hit must be justified by the rule above.

### 11.1 Position formatting

`compiler.Position{File: "flow.nflow", Line: 12}` renders as
`flow.nflow:12`. The CLI, the linter, and modern terminals hyperlink
the result. Always populate `File` and `Line` when you raise a
compile-time error.

### 11.2 Error kinds

Runtime errors should be categorized so retry and circuit-breaker
policies can react correctly. Use the narrowest kind that fits:

- `xerr.NotFound` — the requested resource does not exist.
- `xerr.Validation` — the input is malformed or fails a check.
- `xerr.Unauthorized`, `xerr.Forbidden` — auth/authz.
- `xerr.Conflict` — the operation collided with existing state.
- `xerr.Timeout`, `xerr.Unavailable` — transient infrastructure.
- `xerr.TooManyRequests` — rate limit.
- `xerr.Internal` — unexpected; the fallback.

`xerr.IsTransient(err)` classifies a runtime error for the retry
policy. `xerr.KindFrom(err)` reads the kind in a hook or recovery
handler.

---

## 12. External repositories

Any repository — any language — becomes a Flow extension by adding a
`nexssflow/` directory with a Go shim:

```
my-repo/
├── go.mod                  # module github.com/example/my-repo
└── nexssflow/
    └── library.go            # exposes Bundle(opts) compiler.Bundle
```

Then `@require github.com/example/my-repo v1.2.3` selects the package
`github.com/example/my-repo/nexssflow`. Go determines whether that package
is provided by the root module or by a nested `nexssflow` module; the
requested version applies to whichever module provides it. An explicit
target such as
`@require github.com/example/my-repo/nexssflow_v2 v1.4.0` selects that
package variant, with Go again determining its module provider. Flow does
not infer or truncate module paths from repository URL shape, and remote
requirements remain version-pinned. An unversioned remote subpackage is
reserved for packages already available through the current module or Go
workspace; it does not request `latest`.

The shim can wrap `external.exec` (shell), `external.wasm` (Wazero WASI),
`http.request` (network), or a direct Go import if the repo is already in
Go.

For a monorepo with multiple bundles, name each subdirectory
explicitly:

```
my-repo/
├── nexssflow/           # default
├── nexssflow_dev/       # development mocks
└── nexssflow_prod/      # production hardened
```

Then `@require github.com/example/my-repo/nexssflow_dev v1.2.3` selects
that package path directly.

For rapid local iteration, a directory with `.go` files and no
`go.mod` is accepted as a loose package:

```nflow
@require ./scratch
```

The harness vendors the directory into its temp build; nothing is
written to your workspace.

---

## 13. Patterns

### 13.1 A directive that owns a block

Read the block with `compiler.ReadBlock`, blank its delimiters, install a
line-indexed lookup. See `extensions/scope/directive.go` for the
canonical example.

### 13.2 A primary that installs conditionally

Install from `OnPreprocess`, only when the current source uses your
syntax. See `extensions/macros/library.go`.

### 13.3 A wrapper that publishes context

```go
func wrapFromMeta(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
    cfg, _ := meta["mything"].(MyConfig)
    if cfg.Empty() {
        return inner, nil
    }
    return action.New("mything.wrap", func(ctx context.Context, req any) (any, error) {
        return action.InvokeAny(WithMyConfig(ctx, cfg), inner, req)
    }).Build(), nil
}
```

### 13.4 A materializer that mounts from meta

```go
func materialize(req compiler.MaterializeReq) error {
    for _, d := range metaDeclarations(req.Meta) {
        act, err := buildAction(d, req.Resolver)
        if err != nil {
            return err
        }
        if err := req.Resolver.Mount(action.Library{
            Name:    "mything." + d.Name,
            Actions: []action.AnyAction{act},
        }); err != nil {
            return err
        }
    }
    return nil
}
```

See `extensions/pool/materialize.go` for a full example.

---

## 14. Rules

- Package comment: `// Package <name> ships <what>.` plus a
  `Typical use:` block.
- No `const ImportPath`. No hardcoded module URLs anywhere.
- No silent failures. Collisions panic. Missing contracts fail loudly.
- Every public API returns categorized errors (§11).
- Fixtures before inline `SelfTest`.
- Tests exercise your bundle, not Kernel runtime.
- One responsibility per file. `library.go` wires; feature files
  implement.

---

## Related

- [Language syntax](language.md)
- [CLI and runtime](cli.md)
- [Kernel action builders](https://pkg.go.dev/github.com/nexssp/kernel/action)
- [`extensions/README.md`](../extensions/README.md) — in-tree browsing shortcut
