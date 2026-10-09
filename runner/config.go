package runner

import (
	"fmt"
	"maps"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// BuildConfig compiles bundles into execution configuration.
// It has ZERO knowledge of @require, URLs or module paths.
func BuildConfig(bundles []core.Bundle) (Config, error) {
	unique := make([]core.Bundle, 0, len(bundles))
	seenBundles := make(map[string]struct{}, len(bundles))
	seenLibraries := make(map[string]string)
	seenAliases := make(map[string]string)
	argSchemas := make(map[string][]core.ArgFieldSpec)

	for i := range bundles {
		b := bundles[i]
		if err := core.ValidateBundle(b); err != nil {
			return Config{}, err
		}
		if _, ok := seenBundles[b.ID]; ok {
			return Config{}, fmt.Errorf("duplicate bundle %q", b.ID)
		}
		seenBundles[b.ID] = struct{}{}
		if b.Alias != "" {
			if previous, ok := seenAliases[b.Alias]; ok {
				return Config{}, fmt.Errorf("duplicate @require namespace qualifier %q in bundles %q and %q", b.Alias, previous, b.ID)
			}
			seenAliases[b.Alias] = b.ID
		}
		for j := range b.Libraries {
			lib := b.Libraries[j]
			if prev, ok := seenLibraries[lib.Name]; ok {
				return Config{}, fmt.Errorf("duplicate library %q in bundles %q and %q", lib.Name, prev, b.ID)
			}
			seenLibraries[lib.Name] = b.ID
		}
		if len(b.ArgSchemas) > 0 {
			maps.Copy(argSchemas, b.ArgSchemas)
		}
		unique = append(unique, b)
	}

	var (
		extraDirectives []core.Directive
		extraModifiers  []core.Modifier
		extraOperators  []core.Operator
		extraPrimaries  []core.PrimaryExtension
		materializers   []core.Materializer
		bundleHooks     []action.AnyHook
	)

	resolver, err := core.NewDynamicResolver()
	if err != nil {
		return Config{}, err
	}

	for i := range unique {
		bundle := unique[i]
		extraDirectives = append(extraDirectives, bundle.Directives...)
		extraModifiers = append(extraModifiers, bundle.Modifiers...)
		extraOperators = append(extraOperators, bundle.Operators...)
		extraPrimaries = append(extraPrimaries, bundle.Primaries...)
		bundleHooks = append(bundleHooks, bundle.Hooks...)
		for j := range bundle.Libraries {
			lib := bundle.Libraries[j]
			// Mount with the bundle's compilation-local namespace qualifier.
			if bundle.Alias != "" {
				if err := resolver.MountWithAlias(lib, bundle.Alias); err != nil {
					return Config{}, fmt.Errorf("bundle %q library %q: mount: %w", bundle.ID, lib.Name, err)
				}
			} else {
				if err := resolver.Mount(lib); err != nil {
					return Config{}, fmt.Errorf("bundle %q library %q: mount: %w", bundle.ID, lib.Name, err)
				}
			}
		}
	}

	// Declaration materializers (schema, pool) must mount capabilities before
	// pipeline materialization compiles sub-pipelines that invoke them.
	var pipelineMaterializers []core.Materializer
	for i := range unique {
		u := &unique[i]
		if u.Materialize != nil {
			materializer := u.Materialize
			alias := u.Alias
			wrapped := func(req core.MaterializeReq) error {
				req.Alias = alias
				return materializer(req)
			}
			if u.ID == "pipeline" {
				pipelineMaterializers = append(pipelineMaterializers, wrapped)
			} else {
				materializers = append(materializers, wrapped)
			}
		}
	}
	materializers = append(materializers, pipelineMaterializers...)

	for _, km := range core.KeywordMappings() {
		if _, ok := resolver.Action(km.Target); !ok {
			return Config{}, fmt.Errorf(
				"core: native keyword %q targets missing action %q",
				km.Keyword, km.Target)
		}
	}

	return Config{
		Resolver:      resolver,
		Directives:    core.NewDirectiveTable(extraDirectives...),
		Modifiers:     core.NewModifierTable(extraModifiers...),
		Operators:     core.NewOperatorTable(extraOperators...),
		Primaries:     core.NewPrimaryExtensionTable(extraPrimaries...),
		Materializers: materializers,
		CompileOpts:   core.BundleConfig(unique...),
		ArgSchemas:    argSchemas,
		Hooks:         bundleHooks,
	}, nil
}
