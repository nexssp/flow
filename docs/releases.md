# CLI releases

Prebuilt `nflow` command-line binaries are published on the [GitHub Releases page](https://github.com/nexssp/flow/releases). The release workflow builds Linux, macOS, and Windows binaries for `amd64` and `arm64` from the tagged source. Archives include `README.md`, `LICENSE`, and `NOTICE`; Unix-like targets use `.tar.gz`, and Windows targets use `.zip`.

## Compatibility and behavior notes

- **Compiler-version directive.** Use `@nflow_version`; `@flow_version` is unsupported. The technical `flow_version` extension package, bundle ID, and Go import paths are unchanged.

- **Simple compiler-version ranges.** `@nflow_version ">=0.37.0 <0.40.0"` requires every whitespace-separated comparison to hold (AND). Existing exact/single comparisons and version parsing remain compatible; no OR, wildcard, caret, or tilde range language is added. Development/unknown compiler builds retain their existing bypass.
- **Manual release check.** `nflow version check` prints current/latest using the latest stable GitHub release and suggests a pinned `go install` command only for a newer release. It never updates automatically; plain `nflow version` stays offline. Unknown/development builds and failed lookups do not claim an update is available. See [CLI version help](cli.md#version) for exit codes and limitations.
- **Explicit empty strings stay empty.** Argument decoding no longer replaces an explicitly supplied `""` with the field's default. Omit the argument to use its default; check workflows that previously relied on an empty value selecting it.
- **Build help succeeds.** `nflow build --help` exits with status **0**, not **2**, and does not require an input file. Scripts should treat this as a successful help request rather than a usage error.
- **Local self-bench command.** `nflow self bench` discovers embedded `.nflow` scenarios, supports custom paths, warmup and iteration controls, JSON output, and listing without execution. Built-in scenarios are local and do not call external services; see the [CLI guide](cli.md#benchmark-built-in-scenarios) for metric semantics and custom-flow safety.
- **Task arguments select the requested package.** Package-aware tasks now honor arguments after `--`; for example, `task lint -- ./compiler` lints that package instead of silently falling back to `./...`. With no arguments the default remains `./...`. A successful targeted check is not a full-repository check.
- **Continuation formatting stays clean and idempotent.** `nflow fmt` no longer adds trailing whitespace when an arrow or ternary colon ends a continued line; formatting that source repeatedly now produces the same output.
- **Requirement selectors are managed in source.** `nflow require check`, `update`, and `pin` inspect or edit direct remote `@require` selectors; top-level `nflow pin` aliases `nflow require pin`. Pinning canonicalizes the selector in the `.nflow` file, preserving surrounding source text. See [requirement management](requirements.md).
- **Source edits have a one-stage recovery backup.** A source-changing requirement operation keeps the adjacent `<file>.bak`; `nflow require reset <file>` restores and consumes that backup. A no-op or resolution failure does not change it; each successful edit saves the immediately previous stage.
- **Execution diagnostics go to stderr.** Workflow execution failures and assertion diagnostics are written to `stderr` instead of `stdout`; successful workflow results remain on `stdout`. Scripts that previously parsed failures from `stdout` should read `stderr`.
- **No requirement lock is generated or enforced.** `run`, `build`, and `lint` do not need a backup or `.lock` file. `go.sum` may be generated as temporary Go bookkeeping, not as a project artifact. Pinning direct selectors does not freeze the transitive graph, and updated dependencies can still break compatibility.

## Download and verify

Choose the archive matching your operating system and architecture. Asset names use this pattern, with the leading `v` removed from the release tag:

```text
nflow_<version>_<os>_<arch>.tar.gz
nflow_<version>_checksums.txt
```

Windows archives use `.zip` instead of `.tar.gz`. For a release tagged `v<version>`, the expected Linux amd64 archive is `nflow_<version>_linux_amd64.tar.gz`; confirm that the tag and assets are published on the Releases page before downloading.

The checksum file contains SHA-256 digests. Download it alongside the archive and verify before installing. Replace `<version>` in the filename below with the release version, without the leading `v`:

```sh
sha256sum --ignore-missing --check 'nflow_<version>_checksums.txt'
```

With GNU `sha256sum`, `--ignore-missing` skips archives for other platforms that you did not download. Confirm that your downloaded archive is explicitly reported as `OK`. Checksums detect content mismatches; they are not signatures or independent proof of publisher identity. This release configuration does not generate signed checksums or provenance attestations.

On Windows PowerShell, compare the archive's digest with its matching entry in the checksum file using `Get-FileHash -Algorithm SHA256`.

Extract the archive, place the `nflow` executable in a directory on `PATH` (on Windows, the executable is `nflow.exe`), then check it:

```sh
nflow version
```

## Install with a script

The repository includes installers for Linux/macOS (`install.sh`) and Windows (`install.ps1`). They select the matching release archive for `amd64` or `arm64`, check its SHA-256 digest against the release checksum file before installing, and never need Go.

On Linux or macOS, install the latest stable release into `$HOME/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/nexssp/flow/main/install.sh | sh
```

To install a specific tag, pass it to the script. Use the published release tag in the form `v<version>`. Set `NFLOW_INSTALL_DIR` or pass a second argument to choose a different destination:

Specific versions must be full SemVer values (`MAJOR.MINOR.PATCH`); optional prerelease or build metadata is supported, and a leading `v` or `V` is optional.

```sh
curl -fsSL https://raw.githubusercontent.com/nexssp/flow/main/install.sh | sh -s -- 'v<version>'
# Or: curl -fsSL https://raw.githubusercontent.com/nexssp/flow/main/install.sh | sh -s -- 'v<version>' "$HOME/bin"
```

On Windows, download the script and run it from PowerShell. The default destination is `%LOCALAPPDATA%\Programs\nflow`; the installer attempts to add it to the current user's persistent `PATH` and to the current PowerShell session. If the persistent update fails, installation succeeds with a warning. `-NoPathUpdate` disables both PATH updates.

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/nexssp/flow/main/install.ps1 -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1
# Specific version tag and custom destination:
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Version 'v<version>' -InstallDir "$HOME\bin"
```

Open a new terminal after installation to use `nflow` from `PATH`. Checksums catch archive corruption or mismatches; they are not signatures or independent proof of publisher identity.

Alternatively, install the version selected by Go's module version resolution as described in the [README](../README.md#install). Go's `@latest` may differ from GitHub's latest stable release; see [version-check details](cli.md#version):

```sh
go install github.com/nexssp/flow/cmd/nflow@latest
```

## Release process

A push of a `v*` tag starts the GitHub Actions release workflow. It validates the tag and waits for successful `main` CI on the exact tagged commit, rather than repeating the Go tests and NFlow checks already run by CI. It then invokes GoReleaser on the tagged source to build versioned archives, generate SHA-256 checksums, and publish the GitHub release. A manually dispatched release still runs `go test ./...`, `nflow lint ./spec`, and `nflow test ./spec` before building. Stable release tags follow the `v<version>` format.

### Post-publication check

After the `v<version>` workflow succeeds, confirm the release page contains `nflow_<version>_checksums.txt` and all six platform archives:

| Target | amd64 | arm64 |
|---|---|---|
| Linux | `nflow_<version>_linux_amd64.tar.gz` | `nflow_<version>_linux_arm64.tar.gz` |
| macOS | `nflow_<version>_darwin_amd64.tar.gz` | `nflow_<version>_darwin_arm64.tar.gz` |
| Windows | `nflow_<version>_windows_amd64.zip` | `nflow_<version>_windows_arm64.zip` |

Check that each archive has exactly one matching checksum entry, verify downloaded archives against the checksum file, and smoke-test the pinned installer on Linux/macOS and Windows. The installed binary should report `version: v<version>` when you run `nflow version`.

The authoritative packaging and workflow details are [`.goreleaser.yaml`](../.goreleaser.yaml) and [`.github/workflows/release.yml`](../.github/workflows/release.yml). Release assets are generated by CI; this guide does not describe a separate local signing or publishing process.
