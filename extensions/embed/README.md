# `@embed` static assets

`@embed` accepts either one regular file or a directory under an explicitly configured filesystem root:

```nflow
@require github.com/nexssp/flow/extensions/embed { root: "." }
@embed logo "assets/logo.svg"
@embed web "web/dist"
```

A single file remains a typed `xfile.Ref`. It is opened through the registered resolver, which requires caller and tenant scope and rechecks the digest and size. Passing it to actions still uses the normal typed `( ... )` argument form.

A directory becomes an `embed.web` map of slash-separated paths relative to the selected directory to `[]byte` contents. This is intended for explicit public build output, such as a frontend folder; do not point it at a source tree, repository root, or directory containing secrets. Directory bytes are included in compile metadata and are available to ordinary actions such as `thttp.static`; they do not pass through the scoped file resolver.

Defaults bound each file to 16 MiB, the whole directory to 64 MiB, and the number of files to 4,096. The `@require` options `max_size`, `max_total_size`, and `max_files` set positive integer limits. For example:

```nflow
@require github.com/nexssp/flow/extensions/embed {
  root: ".",
  max_size: "2097152",
  max_total_size: "16777216",
  max_files: "512"
}
@embed web "web/dist"
```

`thttp.static` accepts the directory as `files: embed.web` from a `:raw_route` pipeline. Its default index is `index.html`; `spa: true` falls back to the index only for extensionless paths, while missing assets with an extension remain 404 responses.

Directory handling is covered by `nflows/directory_embed.nflow`, executed in `TestDirectoryEmbedNFlowFixture` with a host-supplied virtual filesystem. The file-only reference and scoped resolver behavior remain covered by the existing embed tests.
