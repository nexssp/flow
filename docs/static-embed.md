# Static file embeds

The `extensions/embed` bundle handles compile-time `@embed name "path"` declarations and runtime file resolution. The asset is read during preprocessing from the bundle's declared `fs.FS` or rooted directory, then emitted as a typed `github.com/nexssp/kernel/xfile.Ref` under the file-local `embed` symbol namespace. Use `embed.name` as a typed value inside an `@{ ... }` argument block; it is not a string parameter or an expression-language variable.

```go
bundle, resolver := embed.BundleWithFileResolver(assetsFS, "flow.static", 8<<20)
_ = resolver // bundle registers it; runner.BuildConfig composes it automatically
```

```nflow
@embed logo "assets/logo.svg"

files.stage @{ file: embed.logo }
```

A reference carries a provider, an opaque ID, SHA-256 version/digest, and expected byte size. The ID is a deterministic opaque hash derived from the validated relative asset path and digest; the path itself is not placed in the reference. At runtime, `embed.Resolver` looks the ID up in its typed `map[xfile.ID]assetReference`, revalidates the stored relative path, reopens the file within the declared `fs.FS`, enforces the configured byte limit, and verifies the version, size, and digest before returning bytes. It does not retain asset bytes. References are untrusted selection/integrity data and are never used as filesystem paths or authorization credentials.

`BundleWithFileResolver` returns both the compiler bundle and its resolver. The bundle includes a provider-scoped resolver binding; `runner.BuildConfig` automatically composes bindings from all bundles, routes by `xfile.Ref.Provider`, and exposes the result through `compiler/contracts.FileResolverFromContext` to runtime actions. A Storage extension can register its own provider with `compiler.BundleWithFileResolver`; applications do not need to write a custom resolver multiplexer. `extensions/files` remains a validation helper for typed `map[string]xfile.Ref` resources and does not store bytes.

Opening a reference requires caller and tenant scope in the request context, as defined by `xfile.ScopeFromContext`. These embedded assets are app-scoped; authorization is not inferred from any field in `xfile.Ref`. Filesystem reads use the configured maximum (16 MiB by default); `embed.Bundle` accepts `root`, `provider`, and `max_size` options. An unset root is an error when `@embed` is used. Paths must be canonical relative paths; traversal and symlinks escaping a rooted directory are rejected. Duplicate names, missing/non-regular assets, and over-limit files fail during preprocessing. Unknown `embed.name` references are rejected during compilation. A rooted directory handle is closed through the bundle's shutdown callback.

Flow pins the Kernel release that provides the `xfile` contract; no machine-specific `replace` directive or local Go workspace is needed.
