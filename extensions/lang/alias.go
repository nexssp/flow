package lang

import "sort"

// Alias maps a tag (e.g. "py") to a (Language.ID, Version) pair.
// The runtime never sees aliases; resolveAlias is the only exit point.
//
// Aliases are derived from runner files at init() via registerAlias — the
// table is built, not hand-maintained. The TestAliasInvariants test
// asserts the derived table matches what the runner files declare.
type Alias struct {
	Tag     string // "py2", "python3", "golang"
	ID      string // canonical language ID, e.g. "python"
	Version string // "", "2", "3"
}

// aliases is the derived table. registerAlias appends to it during
// LoadRunners. Sorted on first read by ensureSorted for stable iteration.
var aliases []Alias

// registerAlias adds an alias entry. Called once per alias in a runner
// file. Duplicate tags panic — two runner files claiming the same alias
// is a config conflict, not a silent override.
func registerAlias(tag, id, version string) {
	if tag == "" {
		return
	}
	for _, a := range aliases {
		if a.Tag == tag {
			panic("lang: alias @" + tag + " claimed by both " + a.ID + " and " + id)
		}
	}
	aliases = append(aliases, Alias{Tag: tag, ID: id, Version: version})
}

// resolveAlias maps a tag to its canonical (ID, Version).
//
// Pure function: no I/O, no globals mutation, no allocation beyond the
// result. Tested exhaustively in alias_test.go. Every @<tag> form in the
// DSL flows through this one function before touching the runtime.
//
// Returns ok=false for unknown tags. The caller wraps the failure in a
// compiler.SourceError at the tag's source position.
func resolveAlias(tag string) (id, version string, ok bool) {
	if tag == "" {
		return "", "", false
	}
	for _, a := range aliases {
		if a.Tag == tag {
			return a.ID, a.Version, true
		}
	}
	return "", "", false
}

// allAliases returns a copy of the alias table, sorted by tag. Used by
// tests and by future `nflow explain lang` output. Callers must not
// mutate the returned slice.
func allAliases() []Alias {
	out := make([]Alias, len(aliases))
	copy(out, aliases)
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out
}
