# Modifiers

For configurable retry strategies and the opt-in retry-all policy, see [Retry configuration](retry.md).

A modifier is one `:name` or `:name=value` annotation on an atom. Modifiers
mutate the atom's `*action.Builder[any, any]` in source order before the
action is built. They are the DSL's only mechanism for per-atom
configuration that is not an argument and not part of the pipeline topology.

```nflow
fetch:timeout=5s:retry=3:auth:role=admin:tag="fast,hot"
```

## Three layers

The modifier surface is split across the parser, compiler constructors, and
the active modifier bundles. The parser records modifier tokens; the compiler
constructors decode the supported value shapes.

| Layer | Location | Owns |
|---|---|---|
| Parsing primitives | [`compiler/modifier.go`](../compiler/modifier.go) | `Duration`, `Int`, `Int32`, `Int64`, `Float64`, `String`, `StringList`, `Flag` |
| Standard modifiers | [`extensions/modifiers_core/`](../extensions/modifiers_core/), [`extensions/modifiers_auth/`](../extensions/modifiers_auth/), and [`extensions/modifiers_meta/`](../extensions/modifiers_meta/) | Kernel policy, authorization, and metadata modifiers such as `:timeout=`, `:retry=`, `:auth`, `:role=`, `:name=`, `:tag=`, `:read_only`, … |
| Extension modifiers | `extensions/<domain>/` | Domain-specific modifiers contributed by extensions |

The compiler provides typed constructors for the DSL value shapes. The
standard modifier bundles and domain extensions register their own names;
the active modifier table determines which are available in a given host.

## Writing one modifier

Every modifier is a one-liner. Pick the primitive that matches the DSL
value shape and pass the builder method.

```go
compiler.Duration("timeout", (*action.Builder[any, any]).Timeout)
compiler.Flag("auth", (*action.Builder[any, any]).RequireAuth)
compiler.Int("status", (*action.Builder[any, any]).SuccessStatus)
compiler.String("name", (*action.Builder[any, any]).Name)
compiler.StringList("tag", func(b *action.Builder[any, any], tags []string) *action.Builder[any, any] {
    return b.Tag(tags...)
})
```

### Method expression vs lambda

Use a method expression when the builder method's signature matches the
modifier's typed callback one-to-one:

```go
compiler.Duration("timeout", (*action.Builder[any, any]).Timeout)
```

Use a lambda when the modifier needs to loop, branch, supply extra
arguments, or do anything the method signature does not encode:

```go
compiler.Int("retry", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
    return b.Retry(n, action.ExponentialJitter(100*time.Millisecond, 30*time.Second))
})
```

Lambdas must return the builder as their final statement. The return
value is discarded by the primitive; the builder mutates in place. The
return type exists only so method expressions type-check.

### Flags

A flag takes no value. `:auth` is `compiler.Flag("auth", …)`. Writing
`:auth=yes` in the DSL is a compile error, not a silently accepted
no-op.

### Strict-mode markers

`:strict` and `:lenient` are read by `compiler/strict.go` through
`hasModifier`, not by the builder. Their apply callbacks are identity
lambdas returning the builder unchanged. They exist so the parser
accepts the syntax and so `compiler.ModifierTable.All()` lists them for the
catalog.

## Registering

Standard modifiers are defined in the `extensions/modifiers_*` bundles.
Bundle modifiers register through `compiler.Bundle.Modifiers`:

```go
func Bundle() compiler.Bundle {
    return compiler.Bundle{
        ID:        ID,
        Libraries: []action.Library{{Name: ID}},
        Modifiers: Modifiers(),
    }
}

func Modifiers() []compiler.Modifier {
    return []compiler.Modifier{
        compiler.Int("breaker_failures", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
            return b.Adaptive(action.AdaptiveConfig{FailureThreshold: n})
        }),
        compiler.Duration("breaker_cooldown", func(b *action.Builder[any, any], d time.Duration) *action.Builder[any, any] {
            return b.Adaptive(action.AdaptiveConfig{ResetTimeout: d})
        }),
    }
}
```

Registration is eager. Duplicate modifier names are rejected while the
modifier table is constructed, not deferred until pipeline execution.

## Rules

1. Never call `time.ParseDuration`, `strconv.*`, or `strings.Split` on a
   modifier payload directly. Use `compiler.Duration`, `compiler.Int`, `compiler.Int32`,
   `compiler.Int64`, `compiler.Float64`, `compiler.String`, `compiler.StringList`, `compiler.Flag`.
2. A modifier either mutates the builder or is a documented marker.
   Metadata tags that look like capabilities (`:breaker=`, `:backoff=`)
   are lies and must be deleted or implemented.
3. One line per modifier. If a modifier needs more than five lines, the
   logic belongs inside a builder method, not inside the modifier.
4. Modifier names are lowercase ASCII. Multi-word names use underscores
   (`:breaker_failures`, not `:breakerFailures`).
