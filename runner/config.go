// Plik: flow/runner/config.go
package runner

import (
	"fmt"
	"slices"

	"github.com/nexssp/flow/core"
)

// BuildConfig compiles bundles into execution configuration.
// It has ZERO knowledge of @require, URLs or module paths.
func BuildConfig(bundles []core.Bundle) (Config, error) {
	unique := make([]core.Bundle, 0, len(bundles))
	seenBundles := make(map[string]struct{}, len(bundles))
	seenLibraries := make(map[string]string)
	seenAliases := make(map[string]string)

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
		unique = append(unique, b)
	}

	var (
		extraDirectives []core.Directive
		extraModifiers  []core.Modifier
		extraOperators  []core.Operator
		extraPrimaries  []core.PrimaryExtension
		materializers   []core.Materializer
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

	for i := range slices.Backward(unique) {
		u := unique[len(unique)-1-i]
		if u.Materialize != nil {
			materializer := u.Materialize
			alias := u.Alias
			materializers = append(materializers, func(req core.MaterializeReq) error {
				req.Alias = alias
				return materializer(req)
			})
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
	}, nil
}
