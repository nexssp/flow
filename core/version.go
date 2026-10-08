package core

// Version is the compiler version string that the @flow_version
// directive compares against. It is populated by the CLI at init time
// from its linker-injected build metadata. When a test or embedded
// runner leaves it unset, VersionUnknown tells the directive to accept
// any constraint — a source build has no release identity to compare.
var Version = VersionUnknown

// VersionUnknown marks a compiler with no release identity. It is the
// default for `go run` and for binaries built without the linker's
// -X github.com/nexssp/flow/cli.Version=... injection.
const VersionUnknown = "dev"
