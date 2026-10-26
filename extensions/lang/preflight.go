package lang

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Preflight is the pre-run gate. Before any LangBlock executes, the
// compiler calls Preflight with every LangBlock in the flow. Preflight
// resolves each (find-only — no install), groups missing interpreters,
// and returns a single Report. If the Report has any Missing entries,
// the flow does not run.
//
// Why find-only here: preflight is a CHECK, not an install trigger. Hosts
// may explicitly call lang.Install or provide an Ensure implementation.
// Preflight surfaces what's missing; it doesn't fix it by default.
//
// Why a single Report: a flow with py, rs, go, and ts that's missing
// rust, typescript, AND python3 should print one consolidated list, not
// fail on the first one and force the user to iterate.
type Preflight struct {
	// Installer, if non-nil, is invoked for each Missing entry before
	// the Report is returned. Use this to enable auto-install: preflight
	// still reports what was missing, but installs first. Default nil
	// (no auto-install — the safe default).
	Installer Ensure
}

// Report is the result of a preflight pass. Zero Missing entries means
// "ready to run." One or more means "abort and show this to the user."
//
//nolint:errname // Report is a public result value that also implements error; changing its name breaks the API.
type Report struct {
	// Checked is every (ID, Version) pair examined, deduped. Includes
	// both present and missing.
	Checked []Check
	// Missing is the subset of Checked where the interpreter was not
	// found and no successful install happened. Length 0 = ready.
	Missing []Check
}

// Check is one language examined during preflight.
type Check struct {
	ID      string
	Version string
	Tag     string // first alias that resolved to this (ID, Version), for hints
	Cmd     string // resolved command, "" if missing
	Err     error  // non-nil if missing
}

// OK returns true if everything resolved.
func (r Report) OK() bool { return len(r.Missing) == 0 }

// SelfTestSkipReason lets the repository self-test runner skip a language
// fixture when an interpreter is absent, while still failing on an unknown
// language or a broken preflight contract.
func (r Report) SelfTestSkipReason() string {
	if r.OK() {
		return ""
	}
	for _, check := range r.Missing {
		if errors.Is(check.Err, ErrNotFound) {
			continue
		}
		if notInstalled, ok := errors.AsType[*NotInstalledError](check.Err); ok {
			_ = notInstalled // Only the match result is needed to classify this error.
			continue
		}
		return ""
	}
	return r.Error()
}

// Error renders the report as a multi-line message suitable for
// compiler.SourceError or CLI stderr. Satisfies the error interface so
// callers can `return preflight.Report{...}` directly.
func (r Report) Error() string {
	if r.OK() {
		return ""
	}
	var b strings.Builder
	b.WriteString("lang preflight: missing interpreters — install before running:\n")
	for _, c := range r.Missing {
		if c.Version != "" {
			fmt.Fprintf(&b, "  lang.%s@%s", c.ID, c.Version)
		} else {
			fmt.Fprintf(&b, "  lang.%s", c.ID)
		}
		if c.Tag != "" {
			fmt.Fprintf(&b, " (keyword %s)", c.Tag)
		}
		var notInstalled *NotInstalledError
		if errors.As(c.Err, &notInstalled) && notInstalled.Cause != nil {
			fmt.Fprintf(&b, " — %s", notInstalled.Cause)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Run executes the preflight pass over the given blocks. Dedupes by
// (ID, Version) so a flow that calls py ten times only checks once.
//
// blocks is the list of LangBlock AST nodes extracted from the flow
// by the compiler's AST validator, which calls Preflight.Run before any
// runtime invocation.
func (p Preflight) Run(blocks []*LangBlock) Report {
	// Dedupe by (ID, Version). Map key is "id@version" or "id".
	seen := map[string]*Check{}
	for _, b := range blocks {
		key := b.ID
		if b.Version != "" {
			key = b.ID + "@" + b.Version
		}
		if _, ok := seen[key]; ok {
			continue
		}
		lang, err := Lookup(b.ID, b.Version)
		if err != nil {
			seen[key] = &Check{ID: b.ID, Version: b.Version, Tag: blockTag(b), Err: err}
			continue
		}
		if cmd, _, handled, depErr := resolveDepsCmd(lang, b.Source, "<source>"); handled {
			seen[key] = &Check{ID: b.ID, Version: b.Version, Tag: blockTag(b), Cmd: cmd, Err: depErr}
			continue
		}
		cmd, args, err := lang.Select(b.Version) // find-only
		c := &Check{
			ID:      b.ID,
			Version: b.Version,
			Tag:     blockTag(b),
			Cmd:     cmd,
			Err:     err,
		}
		_ = args // not reported; Cmd presence is the check
		seen[key] = c

		// Auto-install path: if a hook is set, try it. On success,
		// the Check is updated (no longer Missing). On failure, the
		// NotInstalledError carries the install-failure cause.
		if err != nil && p.Installer != nil {
			if cmd2, args2, err2 := p.Installer.Ensure(lang, b.Version); err2 == nil {
				c.Cmd = cmd2
				c.Err = nil
				_ = args2
			} else {
				c.Err = err2
			}
		}
	}

	// Build sorted output. Stable for tests.
	out := Report{}
	for _, c := range seen {
		out.Checked = append(out.Checked, *c)
		if c.Err != nil {
			out.Missing = append(out.Missing, *c)
		}
	}
	sort.Slice(out.Checked, func(i, j int) bool {
		if out.Checked[i].ID != out.Checked[j].ID {
			return out.Checked[i].ID < out.Checked[j].ID
		}
		return out.Checked[i].Version < out.Checked[j].Version
	})
	sort.Slice(out.Missing, func(i, j int) bool {
		if out.Missing[i].ID != out.Missing[j].ID {
			return out.Missing[i].ID < out.Missing[j].ID
		}
		return out.Missing[i].Version < out.Missing[j].Version
	})
	return out
}

func blockTag(block *LangBlock) string {
	if block.Tag != "" {
		return block.Tag
	}
	return firstAlias(block.ID, block.Version)
}

// firstAlias returns the first alias that resolves to (id, version), or ""
// if none. Used in error messages so the user sees py rather than just
// "python".
func firstAlias(id, version string) string {
	for _, a := range aliases {
		if a.ID == id && a.Version == version {
			return a.Tag
		}
	}
	return ""
}
