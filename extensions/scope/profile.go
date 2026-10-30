package scope

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// profilesKey is the meta slot where @profile declarations live.
const profilesKey = "scope.profiles"

// profileDef is one parsed @profile declaration. Parent names another
// profile whose modifiers are inherited; the child's own modifiers
// override the parent's on the same name.
type profileDef struct {
	parent string
	mods   []string
	quoted []bool
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
var ProfileDirective = compiler.Directive{
	Name:    "profile",
	Example: "@profile reliable :timeout=2s :retry=4",
	Handler: handleProfile,
}

func handleProfile(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	header, err := compiler.ParseDeclarationHeaderWithSpans(req, "profile")
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	name, mods, quoted, next := header.Name, header.Modifiers, header.QuotedModifiers, header.Next
	if !isValidProfileName(name) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@profile: invalid name %q", name)
	}

	parent, mods, quoted, err := extractParent(mods, quoted)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@profile %s: %v", name, err)
	}

	profiles := profilesFromMeta(req.Out)
	if err := req.Declare("profile", name); err != nil {
		return compiler.DirectiveRes{}, err
	}
	if _, dup := profiles[name]; dup {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@profile %s: duplicate declaration", name)
	}

	if profiles == nil {
		profiles = make(map[string]profileDef)
		req.Out[profilesKey] = profiles
	}
	profiles[name] = profileDef{parent: parent, mods: mods, quoted: quoted}

	return compiler.DirectiveRes{Next: next}, nil
}

// extractParent pulls a single :parent=NAME entry out of mods and keeps the
// quote flags aligned with the modifiers that remain.
func extractParent(mods []string, quoted []bool) (parent string, rest []string, restQuoted []bool, err error) {
	for i, m := range mods {
		if compiler.ModifierName(m) != "parent" {
			rest = append(rest, m)
			restQuoted = append(restQuoted, quotedAt(quoted, i))
			continue
		}
		if parent != "" {
			return "", nil, nil, errors.New(":parent may appear at most once")
		}
		_, value, ok := strings.Cut(m, "=")
		if !ok {
			return "", nil, nil, errors.New(":parent requires a value (use :parent=NAME)")
		}
		if value == "" {
			return "", nil, nil, errors.New(":parent requires a profile name")
		}
		parent = value
	}
	return parent, rest, restQuoted, nil
}

// ExpandProfile returns the effective modifier list for a named
// profile: parent modifiers first, then the profile's own modifiers
// overriding on name. Cycles and unknown names are compile errors.
//
// A nil meta, an unknown name, or an unresolved parent returns an
// error naming the missing profile. The full parent chain is included
// in cycle diagnostics.
func ExpandProfile(meta map[string]any, name string) ([]string, error) {
	mods, _, err := ExpandProfileWithQuotes(meta, name)
	return mods, err
}

// ExpandProfileWithQuotes is ExpandProfile plus quote-state metadata aligned
// with the resulting modifier list. It preserves quote semantics through
// parent inheritance and child overrides.
func ExpandProfileWithQuotes(meta map[string]any, name string) (mods []string, quoted []bool, err error) {
	profiles := profilesFromMeta(meta)
	if profiles == nil {
		return nil, nil, fmt.Errorf("unknown profile %q", name)
	}
	return expandProfile(profiles, name, nil)
}

func expandProfile(profiles map[string]profileDef, name string, chain []string) (mods []string, quoted []bool, err error) {
	if slices.Contains(chain, name) {
		return nil, nil, fmt.Errorf("profile cycle: %s → %s",
			strings.Join(chain, " → "), name)
	}
	def, ok := profiles[name]
	if !ok {
		if len(chain) == 0 {
			return nil, nil, fmt.Errorf("unknown profile %q", name)
		}
		return nil, nil, fmt.Errorf("unknown parent profile %q in chain %s",
			name, strings.Join(chain, " → "))
	}

	var parentMods []string
	var parentQuoted []bool
	if def.parent != "" {
		var err error
		parentMods, parentQuoted, err = expandProfile(profiles, def.parent,
			append(slices.Clone(chain), name))
		if err != nil {
			return nil, nil, err
		}
	}
	mods, quoted = mergeModsWithQuotes(parentMods, parentQuoted, def.mods, def.quoted)
	return mods, quoted, nil
}

func mergeModsWithQuotes(parent []string, parentQuoted []bool, child []string, childQuoted []bool) (mods []string, quoted []bool) {
	switch {
	case len(parent) == 0:
		return append([]string(nil), child...), alignedQuotes(child, childQuoted)
	case len(child) == 0:
		return append([]string(nil), parent...), alignedQuotes(parent, parentQuoted)
	}

	out := make([]string, 0, len(parent)+len(child))
	outQuoted := make([]bool, 0, len(parent)+len(child))
	index := make(map[string]int, len(parent))
	for i, m := range parent {
		index[compiler.ModifierName(m)] = len(out)
		out = append(out, m)
		outQuoted = append(outQuoted, quotedAt(parentQuoted, i))
	}
	for i, m := range child {
		n := compiler.ModifierName(m)
		if outIndex, ok := index[n]; ok {
			out[outIndex] = m
			outQuoted[outIndex] = quotedAt(childQuoted, i)
			continue
		}
		index[n] = len(out)
		out = append(out, m)
		outQuoted = append(outQuoted, quotedAt(childQuoted, i))
	}
	return out, outQuoted
}

func alignedQuotes(mods []string, quoted []bool) []bool {
	out := make([]bool, len(mods))
	for i := range mods {
		out[i] = quotedAt(quoted, i)
	}
	return out
}

func quotedAt(quoted []bool, index int) bool {
	return index >= 0 && index < len(quoted) && quoted[index]
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
