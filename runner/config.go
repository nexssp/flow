package runner

import (
	"fmt"
	"maps"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xfile"

	compiler "github.com/nexssp/flow/compiler"
)

// BuildConfig compiles bundles into execution configuration.
// It has ZERO knowledge of @require, URLs or module paths.
func BuildConfig(bundles []compiler.Bundle) (Config, error) {
	collected, err := collectUniqueBundles(bundles)
	if err != nil {
		return Config{}, err
	}

	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		return Config{}, err
	}

	contributions, err := collectBundleContributions(resolver, collected.bundles)
	if err != nil {
		return Config{}, err
	}

	materializers := orderedMaterializers(collected.bundles)
	if err := validateKeywordMappings(resolver); err != nil {
		return Config{}, err
	}

	return Config{
		Resolver:      resolver,
		FileResolver:  newFileResolver(collected.fileResolvers),
		Directives:    compiler.NewDirectiveTable(contributions.directives...),
		Modifiers:     compiler.NewModifierTable(contributions.modifiers...),
		Operators:     compiler.NewOperatorTable(contributions.operators...),
		Primaries:     compiler.NewPrimaryExtensionTable(contributions.primaries...),
		Materializers: materializers,
		CompileOpts:   compiler.BundleConfig(collected.bundles...),
		ArgSchemas:    collected.argSchemas,
		Hooks:         contributions.hooks,
	}, nil
}

type uniqueBundleSet struct {
	bundles       []compiler.Bundle
	fileResolvers map[xfile.Provider][]xfile.Resolver
	argSchemas    map[string][]compiler.ArgFieldSpec
}

func collectUniqueBundles(bundles []compiler.Bundle) (uniqueBundleSet, error) {
	collected := uniqueBundleSet{
		bundles:       make([]compiler.Bundle, 0, len(bundles)),
		fileResolvers: make(map[xfile.Provider][]xfile.Resolver),
		argSchemas:    make(map[string][]compiler.ArgFieldSpec),
	}
	seenBundles := make(map[string]struct{}, len(bundles))
	seenLibraries := make(map[string]string)
	seenAliases := make(map[string]string)

	for i := range bundles {
		bundle := bundles[i]
		if err := compiler.ValidateBundle(bundle); err != nil {
			return uniqueBundleSet{}, err
		}
		if _, ok := seenBundles[bundle.ID]; ok {
			return uniqueBundleSet{}, fmt.Errorf("duplicate bundle %q", bundle.ID)
		}
		seenBundles[bundle.ID] = struct{}{}
		if bundle.Alias != "" {
			if previous, ok := seenAliases[bundle.Alias]; ok {
				return uniqueBundleSet{}, fmt.Errorf("duplicate @require namespace qualifier %q in bundles %q and %q", bundle.Alias, previous, bundle.ID)
			}
			seenAliases[bundle.Alias] = bundle.ID
		}
		for j := range bundle.Libraries {
			library := bundle.Libraries[j]
			if previous, ok := seenLibraries[library.Name]; ok {
				return uniqueBundleSet{}, fmt.Errorf("duplicate library %q in bundles %q and %q", library.Name, previous, bundle.ID)
			}
			seenLibraries[library.Name] = bundle.ID
		}
		if len(bundle.ArgSchemas) > 0 {
			maps.Copy(collected.argSchemas, bundle.ArgSchemas)
		}
		for _, binding := range bundle.FileResolvers {
			collected.fileResolvers[binding.Provider] = append(collected.fileResolvers[binding.Provider], binding.Resolver)
		}
		collected.bundles = append(collected.bundles, bundle)
	}

	return collected, nil
}

type bundleContributions struct {
	directives []compiler.Directive
	modifiers  []compiler.Modifier
	operators  []compiler.Operator
	primaries  []compiler.PrimaryExtension
	hooks      []action.AnyHook
}

func collectBundleContributions(resolver *compiler.DynamicResolver, bundles []compiler.Bundle) (bundleContributions, error) {
	var contributions bundleContributions
	for i := range bundles {
		bundle := bundles[i]
		contributions.directives = append(contributions.directives, bundle.Directives...)
		contributions.modifiers = append(contributions.modifiers, bundle.Modifiers...)
		contributions.operators = append(contributions.operators, bundle.Operators...)
		contributions.primaries = append(contributions.primaries, bundle.Primaries...)
		contributions.hooks = append(contributions.hooks, bundle.Hooks...)
		for j := range bundle.Libraries {
			if err := mountBundleLibrary(resolver, bundle, bundle.Libraries[j]); err != nil {
				return bundleContributions{}, err
			}
		}
	}
	return contributions, nil
}

func mountBundleLibrary(resolver *compiler.DynamicResolver, bundle compiler.Bundle, library action.Library) error {
	var err error
	if bundle.Alias != "" {
		err = resolver.MountWithAlias(library, bundle.Alias)
	} else {
		err = resolver.Mount(library)
	}
	if err != nil {
		return fmt.Errorf("bundle %q library %q: mount: %w", bundle.ID, library.Name, err)
	}
	return nil
}

func orderedMaterializers(bundles []compiler.Bundle) []compiler.Materializer {
	var materializers []compiler.Materializer
	var pipelineMaterializers []compiler.Materializer
	for i := range bundles {
		bundle := &bundles[i]
		if bundle.Materialize == nil {
			continue
		}
		materializer := bundle.Materialize
		alias := bundle.Alias
		wrapped := func(req compiler.MaterializeReq) error {
			req.Alias = alias
			return materializer(req)
		}
		if bundle.ID == "pipeline" {
			pipelineMaterializers = append(pipelineMaterializers, wrapped)
		} else {
			materializers = append(materializers, wrapped)
		}
	}
	return append(materializers, pipelineMaterializers...)
}

func validateKeywordMappings(resolver *compiler.DynamicResolver) error {
	for _, mapping := range compiler.KeywordMappings() {
		if _, ok := resolver.Action(mapping.Target); !ok {
			return fmt.Errorf(
				"core: native keyword %q targets missing action %q",
				mapping.Keyword, mapping.Target)
		}
	}
	return nil
}
