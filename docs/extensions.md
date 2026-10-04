# Flow extensions

Every extension is one self-contained Go package under
`extensions/<name>/` that plugs into the compiler through the
`core.Bundle` contract. Core knows only that contract; the feature
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
├── nflows/               # Optional DSL fixtures, auto-discovered
│   └── basic.nflow
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

    "github.com/nexssp/flow/core"
)

const ID = "hello"

func init() {
    core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
    greeting := action.New("hello.greet", func(ctx context.Context, in any) (any, error) {
        return "hello", nil
    }).Description("Return a greeting").Build()

    return core.Bundle{
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
    Directives   []core.Directive
    Modifiers    []core.Modifier
    Operators    []core.Operator
    Primaries    []core.PrimaryExtension
    Materialize  core.Materializer
    OnPreprocess func(meta map[string]any) core.PreprocessContributions
    AtomAdvise   func(atom *core.Atom, builder *action.Builder[any, any]) error
    WrapPipeline func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error)

    ArgSchemas      map[string][]core.ArgFieldSpec
    AcceptedOptions []string

    SelfTest func() []core.SelfTestSection
    Fixtures fs.FS
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
| `OnPreprocess` | Contribute additional primaries or compile options after preprocessing. |
| `AtomAdvise` | Mutate an atom's builder before it runs. |
| `WrapPipeline` | Wrap the compiled program (catch errors, install hooks, publish context). |
| `ArgSchemas` | Declare which action arguments are capability references. |
| `AcceptedOptions` | Whitelist keys accepted in `@require ... { ... }`. `nil` accepts anything. |
| `SelfTest` | Inline features run by `nflow self test`. |
| `Fixtures` | Embedded `.nflow` files run as features. |

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

The reserved list is in `core/reserved.go` and is not extensible.

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
var Directive = core.Directive{
    Name:    "trace",
    Example: `@trace "starting fetch"`,
    Handler: func(ctx context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
        line := strings.TrimSpace(req.Lines[req.I])
        msg := strings.Trim(strings.TrimPrefix(line, "@trace"), `"' `)
        req.Out["trace"] = append(req.Out["trace"].([]string), msg)
        return core.DirectiveRes{Next: req.I + 1}, nil
    },
}
```

`DirectiveReq` gives you the source lines, the current index, the
`meta` map, the file path, and the base directory.

`DirectiveRes.Next` is the line to continue from. For a multi-line block,
read it with `core.ReadBlock`, which handles inline `{ ... }`, multi-line
`{ ... }`, and trailing-brace forms uniformly.

A directive that owns a **block** (like `@scope`) also sets
`BlankLines`: the 0-based line indices whose output should be empty
even though the source line had content. See `extensions/scope/directive.go`.

---

## 6. Modifiers

A modifier is a `:name` or `:name=value` annotation.

```go
core.Modifier{
    Name:    "budget_usd",
    Owner:   core.OwnerBundle,   // default; required for bundle-owned names
    Apply: func(b *action.Builder[any, any], raw string) error {
        limit, err := strconv.ParseFloat(raw, 64)
        if err != nil {
            return err
        }
        return b.Adaptive(action.AdaptiveConfig{Budget: limit})
    },
}
```

### 6.1 Use the typed constructors

Every modifier should parse its payload through a constructor from
`core/modifier.go`. `core.Duration`, `core.Int`, `core.Int32`,
`core.Int64`, `core.Float64`, `core.String`, `core.StringList`,
`core.Flag`. These set `ValueKind` automatically, so the linter can
catch `:budget=abc` statically.

```go
core.Duration("budget", func(b *action.Builder[any, any], d time.Duration) *action.Builder[any, any] {
    return b.Timeout(d)
})
```

### 6.2 Mark inheritable modifiers

A modifier that should propagate through `@scope` / `@profile` to nested
atoms must be marked `WithInheritable`:

```go
core.WithInheritable(core.Duration("timeout", (*action.Builder[any, any]).Timeout))
```

Metadata modifiers (`:tag`, `:status`, `:route`) are not inheritable.
`nflow explain` and the compiler both honor the distinction.

### 6.3 Mark unique modifiers

A modifier that should appear at most once on an atom is marked
`WithUnique`:

```go
core.WithUnique(core.Duration("timeout", (*action.Builder[any, any]).Timeout))
```

A second occurrence is a compile error:

```
modifier :timeout is not repeatable
```

---

## 7. Argument schemas

Some actions take arguments that name **another capability**. Declare
those fields in `ArgSchemas` and the compiler resolves and validates
them:

```go
func Bundle(_ map[string]string) core.Bundle {
    return core.Bundle{
        ID:        ID,
        Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{distribute}}},
        ArgSchemas: map[string][]core.ArgFieldSpec{
            "distribute.map": {
                {Name: "action", Kind: core.ArgCapabilityRef},
            },
        },
    }
}
```

With that declaration:

```nflow
distribute.map @{ action: noop, items: [1, 2] }     # valid
distribute.map @{ action: runtime.noop, items: [1] } # valid
distribute.map @{ action: "noop", items: [1] }       # compile error
distribute.map @{ action: missing, items: [1] }      # compile error
```

An `ArgCapabilityRefList` field accepts a list of bare identifiers:

```go
{Name: "members", Kind: core.ArgCapabilityRefList},
```

```nflow
dispatch.run @{ members: [noop, const], payload: .data }
```

A field **not** declared in `ArgSchemas` is a plain string. A bare
identifier in an undeclared field stays a string; the compiler does not
guess.

---

## 8. Options in `@require`

A flow can pass options to your bundle:

```nflow
@require github.com/example/fancy { endpoint: "https://api.example", retries: "3" }
```

If `AcceptedOptions` is `nil`, your bundle accepts any key. If it is a
list, unknown keys are rejected:

```go
func Bundle(opts map[string]string) core.Bundle {
    return core.Bundle{
        ID:              ID,
        Libraries:       []action.Library{{Name: ID}},
        AcceptedOptions: []string{"endpoint", "retries"},
    }
}
```

```
@require github.com/example/fancy { endpont: "..." }
  error: @require github.com/example/fancy: unknown option "endpont"
         (accepted: [endpoint, retries])
```

For typed options, use `core.Decode[T]`:

```go
type Options struct {
    Endpoint string        `flow:"endpoint" default:"https://api.example"`
    Retries  int           `flow:"retries"  default:"3"`
    Timeout  time.Duration `flow:"timeout"`
}

func Bundle(raw map[string]string) core.Bundle {
    opts, err := core.Decode[Options](raw)
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

Fixtures are the preferred way to test your bundle. A fixture is a real
`.nflow` file that `nflow self test` runs like any pipeline:

```
extensions/loop/nflows/basic.nflow
```

```
@description "Loop loop(...) until(...)"
@assert: result.n == 3
{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
```

The `@description` names the feature. Fixtures are discovered by
walking the embedded `Fixtures` FS, so adding a file is the whole
change.

Inline `SelfTest()` is for cases a fixture cannot express:

- **Files outside the workspace.** Fixtures can declare a `Files` map
  of names to content; anything else needs `SelfTest` with a
  `t.TempDir`.
- **Live subprocesses or network.** `external.exec` and `http.request`
  are self-tested here, not as fixtures.
- **Runtime middleware with no observable output.** `:cache=` and
  `:retry=` are tested through behavioral fixtures; if a policy has no
  observable effect, inline `SelfTest` with a spy hook is the pattern.

The rule of thumb: if the behavior can be expressed as a `.nflow` file
with an `@assert`, use a fixture.

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
| Compile-time with a source position | `core.SourceError(pos, ...)` |
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

`core.Position{File: "flow.nflow", Line: 12}` renders as
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
├── (Python, Rust, Go, shell, anything)
└── nexssflow/
    ├── go.mod
    └── library.go            # exposes Bundle(opts) core.Bundle
```

Then `@require github.com/example/my-repo` resolves to
`github.com/example/my-repo/nexssflow` at build time. The shim can wrap
`external.exec` (shell), `external.wasm` (Wazero WASI), `http.request`
(network), or a direct Go import if the repo is already in Go.

For a monorepo with multiple bundles, name each subdirectory
explicitly:

```
my-repo/
├── nexssflow/           # default
├── nexssflow_dev/       # development mocks
└── nexssflow_prod/      # production hardened
```

Then `@require github.com/example/my-repo/nexssflow_dev` resolves to
that subdirectory directly.

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

Read the block with `core.ReadBlock`, blank its delimiters, install a
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
func materialize(req core.MaterializeReq) error {
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

- [Language syntax](../docs/language.md)
- [CLI and runtime](../docs/cli.md)
- [Kernel action builders](https://pkg.go.dev/github.com/nexssp/kernel/action)
- [`extensions/README.md`](../extensions/README.md) — in-tree browsing shortcut
