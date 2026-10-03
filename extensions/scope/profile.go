package scope

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nexssp/flow/core"
)

// profilesKey is the meta slot where @profile declarations live.
const profilesKey = "scope.profiles"

// profileDef is one parsed @profile declaration. Parent names another
// profile whose modifiers are inherited; the child's own modifiers
// override the parent's on the same name.
type profileDef struct {
	parent string
	mods   []string
}

func profilesFromMeta(meta map[string]any) map[string]profileDef {
	p, _ := meta[profilesKey].(map[string]profileDef)
	return p
}

// ProfileDirective declares a named, reusable set of modifiers:
//
//	@profile reliable :timeout=2s :retry=4
//	@profile fast     :parent=reliable :timeout=500ms
//
// A profile must be declared before the @pipeline or @scope that
// references it. The parent may be declared either before or after the
// child; only the use site requires the parent to exist by then.
var ProfileDirective = core.Directive{
	Name:    "profile",
	Example: "@profile reliable :timeout=2s :retry=4",
	Handler: handleProfile,
}

func handleProfile(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	rest := strings.TrimSpace(strings.TrimPrefix(line, "@profile"))
	if rest == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@profile requires a name")
	}

	nameEnd := strings.IndexAny(rest, ": \t")
	var name, tail string
	if nameEnd < 0 {
		name = rest
	} else {
		name = strings.TrimSpace(rest[:nameEnd])
		tail = rest[nameEnd:]
	}
	if name == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@profile: missing name")
	}
	if !isValidProfileName(name) {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@profile: invalid name %q", name)
	}

	mods := parseScopeHeader(tail)
	parent, mods, err := extractParent(mods)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@profile %s: %v", name, err)
	}

	profiles := profilesFromMeta(req.Out)
	if profiles == nil {
		profiles = make(map[string]profileDef)
		req.Out[profilesKey] = profiles
	}
	if _, dup := profiles[name]; dup {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@profile %s: duplicate declaration", name)
	}
	profiles[name] = profileDef{parent: parent, mods: mods}

	return core.DirectiveRes{Next: req.I + 1}, nil
}

// extractParent pulls a single :parent=NAME entry out of mods.
// A duplicate, a bare :parent, or an empty value is an error.
// extractParent pulls a single :parent=NAME entry out of mods.
// A duplicate, a bare :parent, or an empty value is an error.
func extractParent(mods []string) (parent string, rest []string, err error) {
	for _, m := range mods {
		if core.ModifierName(m) != "parent" {
			rest = append(rest, m)
			continue
		}
		if parent != "" {
			return "", nil, errors.New(":parent may appear at most once")
		}
		_, value, ok := strings.Cut(m, "=")
		if !ok {
			return "", nil, errors.New(":parent requires a value (use :parent=NAME)")
		}
		if value == "" {
			return "", nil, errors.New(":parent requires a profile name")
		}
		parent = value
	}
	return parent, rest, nil
}

// ExpandProfile returns the effective modifier list for a named
// profile: parent modifiers first, then the profile's own modifiers
// overriding on name. Cycles and unknown names are compile errors.
//
// A nil meta, an unknown name, or an unresolved parent returns an
// error naming the missing profile. The full parent chain is included
// in cycle diagnostics.
func ExpandProfile(meta map[string]any, name string) ([]string, error) {
	profiles := profilesFromMeta(meta)
	if profiles == nil {
		return nil, fmt.Errorf("unknown profile %q", name)
	}
	return expandProfile(profiles, name, nil)
}

func expandProfile(profiles map[string]profileDef, name string, chain []string) ([]string, error) {
	if slices.Contains(chain, name) {
		return nil, fmt.Errorf("profile cycle: %s → %s",
			strings.Join(chain, " → "), name)
	}
	def, ok := profiles[name]
	if !ok {
		if len(chain) == 0 {
			return nil, fmt.Errorf("unknown profile %q", name)
		}
		return nil, fmt.Errorf("unknown parent profile %q in chain %s",
			name, strings.Join(chain, " → "))
	}

	var parentMods []string
	if def.parent != "" {
		var err error
		parentMods, err = expandProfile(profiles, def.parent,
			append(slices.Clone(chain), name))
		if err != nil {
			return nil, err
		}
	}
	return mergeMods(parentMods, def.mods), nil
}

// mergeMods merges parent and child modifier lists. Child wins on name
// collision. Order: parent-first, child appended after; first-seen
// position is preserved for names present in both.
func mergeMods(parent, child []string) []string {
	switch {
	case len(parent) == 0:
		return append([]string(nil), child...)
	case len(child) == 0:
		return append([]string(nil), parent...)
	}

	out := make([]string, 0, len(parent)+len(child))
	index := make(map[string]int, len(parent))
	for _, m := range parent {
		index[core.ModifierName(m)] = len(out)
		out = append(out, m)
	}
	for _, m := range child {
		n := core.ModifierName(m)
		if i, ok := index[n]; ok {
			out[i] = m
			continue
		}
		index[n] = len(out)
		out = append(out, m)
	}
	return out
}

func isValidProfileName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] != '_' && !isASCIILetter(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if c != '_' && !isASCIILetter(c) && !isASCIIDigit(c) {
			return false
		}
	}
	return true
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
