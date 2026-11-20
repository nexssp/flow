# Compile-time constants and runtime data

Flow has both compile-time text substitution and runtime values. They are
different mechanisms; neither is a general expression-interpolation system.

## Compile-time constants

`@const` declares a string value. During preprocessing, `${NAME}` references
are replaced textually in source lines that follow the declaration. A loaded
JSON object can be exposed as a named set of constants with `@const.load`:

```nflow
@const SUPPORT_QUEUE = "support.priority"
@const.load "settings.json" as settings

"${SUPPORT_QUEUE}"
```

If `settings.json` contains `{"queue":"support.review"}`,
`${settings.queue}` is replaced with that string. The file path is resolved
relative to the source file. `@const.load ... :schema=Settings` can validate
the loaded object against a schema declared earlier in the same source module.
Substitution is textual: quote and escape the replacement appropriately for
the NFlow syntax where it is inserted. Constants are module-local; `@include`
does not export an included module's constants into its parent.

These constants are useful for values that must be fixed in source before
compilation, including modifier values and static action arguments. They are
not runtime lookups and do not perform arithmetic or concatenate expressions.

`@config` and `@config.load` are separate directives. They populate compile-time
configuration metadata; they do not automatically become fields in the runtime
input or expression environment. Use `${...}` only where constant substitution
is intended, and pass runtime values through the pipeline input instead.

When a config key is available to the compiler, `@config.KEY` can also appear
inside an expression. The compiler replaces it with an escaped string literal
before validating and compiling the expression, so it remains a compile-time
value rather than a runtime lookup:

```nflow
@config:base="/srv/files"
{ path: @config.base + "/" + .path }
```

An expression that names an unknown config key fails compilation. Config values
are strings in expressions; quote or convert them as needed for the operation.

## Runtime data

Runtime values are supplied by the current input, action results, or runtime
capabilities. Use `.field` paths in projections, conditions, and supported
argument references; use `runtime.env` when a flow must read an environment
variable at execution time. A projection is an expression context:

```nflow
{ amount: 120, tenant_id: "acme" }
-> { subject: "audit." + .tenant_id, flagged: .amount > 100 }
```

Parenthesized action calls such as `action(field: value)` carry typed values and
references; they are not general-purpose expression contexts. For calculated
values, compute them in a projection or use an explicitly parenthesized
expression where the receiving action supports it, rather than writing an
unparenthesized expression such as `"prefix" + .field` as an argument value.

## Module imports

`@include` imports a source module; it does not textually inline the file or
merge its compile-time declarations into the parent. Constants remain local to
their declaring module. Use `@include` to reuse exported module capabilities,
not as a shared-constants mechanism. `@require` independently loads extension
bundles.

```nflow
@include "payments.nflow" AS billing
@require github.com/example/transport/nexssflow v1.2.3 as transport
```

The `@require` example is illustrative: that package must exist and be available
to the CLI harness or host. See the [language reference](language-reference.md)
for the active-bundle and runtime boundaries.
