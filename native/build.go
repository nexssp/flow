package native

import (
	"fmt"

	"github.com/nexssp/flow/compiler"
	requireext "github.com/nexssp/flow/extensions/require"
)

const shippedExtensionImportPrefix = "github.com/nexssp/flow/extensions/"

// BuildBundle identifies a shipped Go package needed by a generated build.
type BuildBundle struct {
	ID         string
	ImportPath string
}

// PlanBuild composes the shipped preset, mandatory grammar providers and
// extension-owned contributions. Optional bundles require an explicit @require.
// The caller supplies fragments from the existing preprocessing lifecycle.
func PlanBuild(sources []compiler.BuildSource, requirements []requireext.Requirement, all bool) ([]BuildBundle, error) {
	defaults := Bundles()
	descriptors := SelftestBundlesFrom(defaults)
	roots, err := grammarBuildRoots(defaults)
	if err != nil {
		return nil, err
	}
	if all {
		for i := range defaults {
			roots = append(roots, defaults[i].ID)
		}
	} else {
		selected, usageErr := compiler.AnalyzeBuildUsage(defaults, sources)
		if usageErr != nil {
			return nil, usageErr
		}
		roots = append(roots, selected...)
	}
	explicit, err := explicitBuildRoots(descriptors, buildRequirementTargets(sources, requirements))
	if err != nil {
		return nil, err
	}
	roots = append(roots, explicit...)
	resolved, err := compiler.ResolveBuildBundles(descriptors, roots)
	if err != nil {
		return nil, err
	}
	out := make([]BuildBundle, 0, len(resolved))
	for i := range resolved {
		id := resolved[i].ID
		out = append(out, BuildBundle{ID: id, ImportPath: shippedExtensionImportPrefix + id})
	}
	return out, nil
}

// BuildConfig validates all native keyword targets at boot, not only words used
// in this source. Derive those mandatory providers from the canonical grammar
// bindings and actual action registrations. Do not maintain another bundle list.
func grammarBuildRoots(bundles []compiler.Bundle) ([]string, error) {
	owners := make(map[string]string)
	for i := range bundles {
		bundle := &bundles[i]
		for j := range bundle.Libraries {
			for _, registered := range bundle.Libraries[j].Actions {
				if meta := registered.Describe(); meta != nil {
					owners[meta.Name] = bundle.ID
				}
			}
		}
	}
	seen := make(map[string]bool)
	var roots []string
	for _, binding := range compiler.KeywordMappings() {
		owner, exists := owners[binding.Target]
		if !exists {
			return nil, fmt.Errorf("native build: grammar keyword %q targets missing action %q", binding.Keyword, binding.Target)
		}
		if !seen[owner] {
			seen[owner] = true
			roots = append(roots, owner)
		}
	}
	return roots, nil
}

func buildRequirementTargets(sources []compiler.BuildSource, requirements []requireext.Requirement) map[string]bool {
	targets := make(map[string]bool)
	collect := func(entries []requireext.Requirement) {
		for i := range entries {
			target := entries[i].PackagePath
			if target == "" {
				target = entries[i].Import
			}
			if target != "" {
				targets[target] = true
			}
		}
	}
	collect(requirements)
	for _, source := range sources {
		collect(requireext.FromMeta(source.Metadata))
	}
	return targets
}

func explicitBuildRoots(bundles []compiler.Bundle, targets map[string]bool) ([]string, error) {
	explicit := make(map[string]bool)
	var roots []string
	for i := range bundles {
		id := bundles[i].ID
		if targets[id] || targets[shippedExtensionImportPrefix+id] {
			explicit[id] = true
			roots = append(roots, id)
		}
	}
	for i := range bundles {
		bundle := &bundles[i]
		if !explicit[bundle.ID] {
			continue
		}
		for _, replaced := range bundle.Build.Replaces {
			if explicit[replaced] {
				return nil, fmt.Errorf("native build: @require %s conflicts with @require %s; %s replaces %s", replaced, bundle.ID, bundle.ID, replaced)
			}
		}
	}
	return roots, nil
}
