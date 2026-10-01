# Modifiers

A modifier is one `:name` or `:name=value` annotation on an atom. Modifiers
mutate the atom's `*action.Builder[any, any]` in source order before the
action is built. They are the DSL's only mechanism for per-atom
configuration that is not an argument and not part of the pipeline topology.

```nflow
fetch:timeout=5s:retry=3:auth:role=admin:tag="fast,hot"
```

## Three layers

The modifier surface is deliberately split across three places. Nothing
else parses `:name=value` strings.

| Layer | Location | Owns |
|---|---|---|
| Parsing primitives | `flow/core/modifier.go` | `Duration`, `Int`, `Int32`, `Int64`, `Float64`, `String`, `StringList`, `Flag` |
| Standard modifiers | `flow/modifiers/standard.go` | `:timeout=`, `:retry=`, `:cache=`, `:auth`, `:role=`, `:name=`, `:tag=`, `:read_only`, … |
| Extension modifiers | `flow/extensions/<domain>/modifiers.go` | Everything else: `:breaker_failures=`, `:budget=`, `:model=`, `:nats=`, … |

Core owns the parsing rules for the seven DSL value shapes. Extensions own
their domain. Nothing in between duplicates either.

## Writing one modifier

Every modifier is a one-liner. Pick the primitive that matches the DSL
value shape and pass the builder method.

```go
core.Duration("timeout", (*action.Builder[any, any]).Timeout)
core.Flag("auth", (*action.Builder[any, any]).RequireAuth)
core.Int("status", (*action.Builder[any, any]).SuccessStatus)
core.String("name", (*action.Builder[any, any]).Name)
core.StringList("tag", func(b *action.Builder[any, any], tags []string) *action.Builder[any, any] {
    return b.Tag(tags...)
})
```

### Method expression vs lambda

Use a method expression when the builder method's signature matches the
modifier's typed callback one-to-one:

```go
core.Duration("timeout", (*action.Builder[any, any]).Timeout)
```

Use a lambda when the modifier needs to loop, branch, supply extra
arguments, or do anything the method signature does not encode:

```go
core.Int("retry", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
    return b.Retry(n, action.ExponentialJitter(100*time.Millisecond, 30*time.Second))
})
```

Lambdas must return the builder as their final statement. The return
value is discarded by the primitive; the builder mutates in place. The
return type exists only so method expressions type-check.

### Flags

A flag takes no value. `:auth` is `core.Flag("auth", …)`. Writing
`:auth=yes` in the DSL is a compile error, not a silently accepted
no-op.

### Strict-mode markers

`:strict` and `:lenient` are read by `core/strict.go` through
`hasModifier`, not by the builder. Their apply callbacks are identity
lambdas returning the builder unchanged. They exist so the parser
accepts the syntax and so `core.ModifierTable.All()` lists them for the
catalog.

## Registering

Standard modifiers register through `flow/modifiers/standard.go`. Bundle
modifiers register through `core.Bundle.Modifiers`:

```go
func Bundle() core.Bundle {
    return core.Bundle{
        Library:   action.Library{Name: ImportPath},
        Modifiers: Modifiers(),
    }
}

func Modifiers() []core.Modifier {
    return []core.Modifier{
        core.Int("breaker_failures", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
            return b.Adaptive(action.AdaptiveConfig{FailureThreshold: n})
        }),
        core.Duration("breaker_cooldown", func(b *action.Builder[any, any], d time.Duration) *action.Builder[any, any] {
            return b.Adaptive(action.AdaptiveConfig{ResetTimeout: d})
        }),
    }
}
```

Registration is eager. A duplicate name — two modifiers claiming `:retry`
across two bundles — panics at table construction, not at pipeline
execution.

## Rules

1. Never call `time.ParseDuration`, `strconv.*`, or `strings.Split` on a
   modifier payload directly. Use `core.Duration`, `core.Int`, `core.Int32`,
   `core.Int64`, `core.Float64`, `core.String`, `core.StringList`, `core.Flag`.
2. A modifier either mutates the builder or is a documented marker.
   Metadata tags that look like capabilities (`:breaker=`, `:backoff=`)
   are lies and must be deleted or implemented.
3. One line per modifier. If a modifier needs more than five lines, the
   logic belongs inside a builder method, not inside the modifier.
4. Modifier names are lowercase ASCII. Multi-word names use underscores
   (`:breaker_failures`, not `:breakerFailures`).
