package cli

import (
	"io"

	"github.com/nexssp/flow/builder"
	core "github.com/nexssp/flow/compiler"
)

// The exported fields remain for source compatibility; build metadata is owned by builder.
var (
	Version = builder.Version
	Commit  = builder.Commit
	BuiltAt = builder.BuiltAt
)

func init() { core.Version = builder.Version }

func Print(w io.Writer) { builder.Print(w) }

func resolvedBuildInfo() (version, commit, builtAt string) {
	return builder.ResolvedBuildInfo()
}
