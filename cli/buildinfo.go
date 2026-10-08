package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/nexssp/flow/core"
)

func init() {
	core.Version = Version
}

// Build metadata. Injected at build time with:
//
//	go build -ldflags="-X github.com/nexssp/flow/cli.Version=v1.2.3 ..."
//
// When the linker does not inject them, resolvedBuildInfo fills them
// from the module and VCS information that Go embeds automatically.
const unknownValue = "unknown"

var (
	Version = "dev"
	Commit  = unknownValue
	BuiltAt = unknownValue
)

// Print writes a multi-line build summary to w.
func Print(w interface{ Write([]byte) (int, error) }) {
	v, c, b := resolvedBuildInfo()
	fmt.Fprintf(w, "nflow\n")
	fmt.Fprintf(w, "  version:   %s\n", v)
	fmt.Fprintf(w, "  commit:    %s\n", c)
	fmt.Fprintf(w, "  built at:  %s\n", b)
	fmt.Fprintf(w, "  go:        %s\n", runtime.Version())
	fmt.Fprintf(w, "  os/arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func resolvedBuildInfo() (version, commit, builtAt string) {
	version, commit, builtAt = Version, Commit, BuiltAt

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit, builtAt
	}
	if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == unknownValue || commit == "" {
				commit = s.Value
				if len(commit) > 12 {
					commit = commit[:12]
				}
			}
		case "vcs.time":
			if builtAt == unknownValue || builtAt == "" {
				builtAt = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" && commit != "" {
				commit += "-dirty"
			}
		}
	}
	if commit == "" {
		commit = unknownValue
	}
	if builtAt == "" {
		builtAt = unknownValue
	}
	return version, commit, builtAt
}
