package runner

import (
	"github.com/nexssp/flow/core"
)

// PrimariesFor returns the parser primary table the compiler installs
// for a source that produced meta. Parent primaries, when supplied, are
// inserted between the base table and the source's own contributions,
// mirroring CompileReq.InheritedPrimaries.
//
// Every call site that parses a source outside of CompileAction —
// nflow lint, nflow expand, and any future diagnostic tool — must go
// through this function. Reading cfg.Primaries directly is only correct
// for the base table, and doing so silently drops per-source primaries
// such as the macro engine. A linter that does this rejects a file the
// runtime accepts, which is exactly the class of "test and production
// disagree" bug the runner package exists to prevent.
//
// Order of contribution:
//
//  1. c.Primaries                 — the base table built at BuildConfig
//  2. parent...                   — inherited from an outer source
//  3. meta's OnPreprocess output  — this source's own contributions
//
// A MergeablePrimary in step 2 or 3 collapses with the same token in
// step 1 or 2. A plain primary collides and panics, which is the
// "duplicates fail loudly" rule.
func (c Config) PrimariesFor(
	meta map[string]any,
	parent ...core.PrimaryExtension,
) *core.PrimaryExtensionTable {
	var all []core.PrimaryExtension
	if c.Primaries != nil {
		all = append(all, c.Primaries.All()...)
	}
	all = append(all, parent...)
	all = append(all, core.PreprocessContributionsFromMeta(meta, c.CompileOpts...).Primaries...)
	return core.NewPrimaryExtensionTable(all...)
}
