package compiler

import (
	"fmt"

	"github.com/nexssp/kernel/xerr"
)

// BuildUsage is one source contribution considered by the generated-build
// planner. Raw preserves syntax inside pipeline/macro bodies that preprocessing
// may remove; Clean includes constants and other substitutions.
type BuildUsage struct {
	File       string
	Raw        string
	Clean      string
	Metadata   map[string]any
	Directives []string
}

// BuildSource is the source-oriented spelling used by build planners.
type BuildSource = BuildUsage

// AnalyzeBuildUsage returns selected bundle IDs in input bundle order. Matching
// is conservative and token based: any exact contributed name can select a
// bundle, even where a full parser could prove the symbol is unreachable.
func AnalyzeBuildUsage(bundles []Bundle, usages []BuildUsage) ([]string, error) {
	roots := make([]string, 0, len(bundles))
	for i := range bundles {
		b := &bundles[i]
		if b.ID == "" {
			return nil, xerr.BadRequest("build usage: bundle ID is required")
		}
		if b.Build.Always {
			roots = append(roots, b.ID)
		}
	}
	for _, usage := range usages {
		names := make(map[string]bool)
		types := make(map[TokenType]bool)
		modifiers := make(map[string]bool)
		collectBuildTokens(usage.Raw, names, types, modifiers)
		collectBuildTokens(usage.Clean, names, types, modifiers)
		for i := range bundles {
			b := &bundles[i]
			if b.Build.Sources == nil {
				continue
			}
			for _, source := range b.Build.Sources(usage.Metadata) {
				collectBuildTokens(source, names, types, modifiers)
			}
		}
		for i := range bundles {
			b := &bundles[i]
			if bundleUsed(*b, usage, names, types, modifiers) {
				roots = append(roots, b.ID)
			}
		}
	}
	resolved, err := ResolveBuildBundles(bundles, roots)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(resolved))
	for i := range resolved {
		ids[i] = resolved[i].ID
	}
	return ids, nil
}

func collectBuildTokens(source string, names map[string]bool, types map[TokenType]bool, modifiers map[string]bool) {
	var collect func(string)
	collect = func(input string) {
		var previous Token
		for lexer := NewLexer(input); ; {
			tok := lexer.Next()
			if tok.Type == TokEOF {
				break
			}
			types[tok.Type] = true
			if tok.Type == TokIdent {
				names[tok.Lit] = true
				if translated, ok := TranslateKeyword(tok.Lit); ok {
					names[translated] = true
				}
				if previous.Type == TokColon {
					modifiers[tok.Lit] = true
				}
			}
			// @{...} contains embedded Flow syntax and is opaque to the
			// enclosing lexer; re-lex its body without treating strings as names.
			if tok.Type == TokAtBrace {
				collect(tok.Lit)
			}
			previous = tok
		}
	}
	collect(source)
}

func bundleUsed(b Bundle, usage BuildUsage, names map[string]bool, types map[TokenType]bool, modifiers map[string]bool) bool {
	return bundleUsesDirectives(b, usage.Directives) || bundleUsesLibraries(b, names) ||
		bundleUsesOperators(b, types) || bundleUsesPrimaries(b, names, types) || bundleUsesModifiers(b, modifiers)
}

func bundleUsesDirectives(b Bundle, directives []string) bool {
	for _, directive := range directives {
		for _, candidate := range b.Directives {
			if directive == candidate.Name {
				return true
			}
		}
	}
	return false
}

func bundleUsesLibraries(b Bundle, names map[string]bool) bool {
	for i := range b.Libraries {
		lib := &b.Libraries[i]
		if names[lib.Name] {
			return true
		}
		for _, action := range lib.Actions {
			if meta := action.Describe(); meta != nil && names[meta.Name] {
				return true
			}
		}
		for _, source := range lib.Sources {
			if meta := source.Describe(); meta != nil && names[meta.Name] {
				return true
			}
		}
		for _, operator := range lib.Operators {
			if names[operator.Name] {
				return true
			}
		}
		for _, alias := range lib.Aliases {
			if names[alias.Canonical] || anyBuildToken(names, alias.Short) {
				return true
			}
		}
	}
	return false
}

func bundleUsesOperators(b Bundle, types map[TokenType]bool) bool {
	for _, op := range b.Operators {
		if types[op.Meta.Token] {
			return true
		}
	}
	return false
}

func bundleUsesPrimaries(b Bundle, names map[string]bool, types map[TokenType]bool) bool {
	for _, primary := range b.Primaries {
		if p, ok := primary.(TokenPrimary); ok && types[p.TokenType()] {
			return true
		}
		if p, ok := primary.(KeywordPrimary); ok && names[p.Keyword()] {
			return true
		}
		if p, ok := primary.(PostfixExtension); ok && types[p.PostfixToken()] {
			return true
		}
	}
	return false
}

func bundleUsesModifiers(b Bundle, modifiers map[string]bool) bool {
	for _, modifier := range b.Modifiers {
		if modifiers[modifier.Name] {
			return true
		}
	}
	for _, modifier := range b.Build.Modifiers {
		if modifiers[modifier] {
			return true
		}
	}
	return false
}

func anyBuildToken(names map[string]bool, candidates []string) bool {
	for _, candidate := range candidates {
		if names[candidate] {
			return true
		}
	}
	return false
}

// ResolveBuildBundles closes Requires, removes replaced bundles, and validates
// cycles, missing IDs, duplicates, and replacements that break requirements.
func ResolveBuildBundles(bundles []Bundle, selected []string) ([]Bundle, error) {
	byID, err := indexBuildBundles(bundles)
	if err != nil {
		return nil, err
	}
	wanted, err := requiredBuildBundles(byID, selected)
	if err != nil {
		return nil, err
	}
	if err := applyBuildReplacements(byID, wanted); err != nil {
		return nil, err
	}
	out := make([]Bundle, 0, len(wanted))
	for i := range bundles {
		b := &bundles[i]
		if wanted[b.ID] {
			out = append(out, *b)
		}
	}
	return out, nil
}

func indexBuildBundles(bundles []Bundle) (map[string]Bundle, error) {
	byID := make(map[string]Bundle, len(bundles))
	for i := range bundles {
		b := &bundles[i]
		if b.ID == "" {
			return nil, xerr.BadRequest("build bundle: ID is required")
		}
		if _, dup := byID[b.ID]; dup {
			return nil, fmt.Errorf("build bundle: duplicate ID %q", b.ID)
		}
		byID[b.ID] = *b
	}
	return byID, nil
}

func requiredBuildBundles(byID map[string]Bundle, selected []string) (map[string]bool, error) {
	wanted, visiting := make(map[string]bool), make(map[string]bool)
	for _, id := range selected {
		if err := addRequiredBuildBundle(id, byID, wanted, visiting); err != nil {
			return nil, err
		}
	}
	return wanted, nil
}

func addRequiredBuildBundle(id string, byID map[string]Bundle, wanted, visiting map[string]bool) error {
	b, ok := byID[id]
	if !ok {
		return fmt.Errorf("build bundle: selected bundle %q is missing", id)
	}
	if visiting[id] {
		return fmt.Errorf("build bundle dependency cycle includes %q", id)
	}
	if wanted[id] {
		return nil
	}
	visiting[id] = true
	for _, dep := range b.Build.Requires {
		if _, ok := byID[dep]; !ok {
			return fmt.Errorf("build bundle %q requires missing bundle %q", id, dep)
		}
		if err := addRequiredBuildBundle(dep, byID, wanted, visiting); err != nil {
			return err
		}
	}
	delete(visiting, id)
	wanted[id] = true
	return nil
}

func applyBuildReplacements(byID map[string]Bundle, wanted map[string]bool) error {
	replacing, err := buildReplacementRoots(byID, wanted)
	if err != nil {
		return err
	}
	if err := checkBuildReplacementCycles(byID, wanted, replacing); err != nil {
		return err
	}
	for id := range replacedBuildBundles(byID, wanted) {
		delete(wanted, id)
	}
	return validateBuildRequirements(byID, wanted)
}

func buildReplacementRoots(byID map[string]Bundle, wanted map[string]bool) (map[string]bool, error) {
	replacing := make(map[string]bool)
	for id := range wanted {
		for _, target := range byID[id].Build.Replaces {
			if _, ok := byID[target]; !ok {
				return nil, fmt.Errorf("build bundle %q replaces missing bundle %q", id, target)
			}
			if wanted[target] {
				replacing[id] = true
			}
		}
	}
	return replacing, nil
}

func checkBuildReplacementCycles(byID map[string]Bundle, wanted, replacing map[string]bool) error {
	visiting, visited := make(map[string]bool), make(map[string]bool)
	for id := range replacing {
		if err := checkBuildReplacementCycle(id, byID, wanted, visiting, visited); err != nil {
			return err
		}
	}
	return nil
}

func checkBuildReplacementCycle(id string, byID map[string]Bundle, wanted, visiting, visited map[string]bool) error {
	if visiting[id] {
		return fmt.Errorf("build bundle replacement cycle includes %q", id)
	}
	if visited[id] {
		return nil
	}
	visiting[id] = true
	for _, target := range byID[id].Build.Replaces {
		if wanted[target] {
			if err := checkBuildReplacementCycle(target, byID, wanted, visiting, visited); err != nil {
				return err
			}
		}
	}
	delete(visiting, id)
	visited[id] = true
	return nil
}

func replacedBuildBundles(byID map[string]Bundle, wanted map[string]bool) map[string]bool {
	removed := make(map[string]bool)
	for id := range wanted {
		for _, target := range byID[id].Build.Replaces {
			if wanted[target] {
				removed[target] = true
			}
		}
	}
	return removed
}

func validateBuildRequirements(byID map[string]Bundle, wanted map[string]bool) error {
	for id := range wanted {
		for _, dep := range byID[id].Build.Requires {
			if !wanted[dep] {
				return fmt.Errorf("build bundle %q replaces required bundle %q", id, dep)
			}
		}
	}
	return nil
}
