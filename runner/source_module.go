package runner

import (
	"context"
	"fmt"
	"sort"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

func mountSourceModule(cfg Config, parent compiler.CapabilityResolver, imported compiler.SourceImport, child Compilation) error {
	local, ok := child.Resolver.(*executionResolver)
	if !ok {
		return compiler.SourceError(imported.Position, "@include: source resolver is unavailable")
	}
	libraries := local.sourceDeclarations()
	if child.Program != nil {
		libraries = append(libraries, action.Library{
			Name:    "source body " + sourceName(imported.Source),
			Actions: []action.AnyAction{action.Dynamic(child.Program).Name("__source__").Build()},
		})
	}
	symbols := compiler.SymbolsFromMeta(child.Meta)
	bind := func(ctx context.Context) context.Context {
		ctx = compiler.WithLocalSymbols(ctx, symbols)
		return withExecutionContext(ctx, cfg, child.Resolver, child.Meta)
	}
	root, ok := parent.(*executionResolver)
	if !ok {
		return compiler.SourceError(imported.Position, "@include: parent source resolver is unavailable")
	}
	// Exports already contain compiled actions with their hooks. Do not attach
	// those hooks again at each namespace boundary of a nested import.
	root.mu.Lock()
	defer root.mu.Unlock()
	for i := range libraries {
		lib := &libraries[i]
		lib.Name = fmt.Sprintf("%s AS %s (%s)", lib.Name, imported.Namespace, imported.Position)
		qualified := compiler.QualifySourceLibrary(*lib, imported.Namespace, bind)
		if err := root.mountLocked(qualified, "", nil); err != nil {
			return compiler.SourceError(imported.Position, "@include: %w", err)
		}
	}
	root.invalidateAllSnapshotsLocked()
	return nil
}

func sourceName(prepared *compiler.PreparedSource) string {
	name, _ := prepared.SourceIdentity()
	return name
}

// sourceDeclarations excludes cached lookups of base libraries: importing a
// source must not accidentally re-export its external capabilities. Each
// declaration keeps its owner so a collision retains its definition location.
func (r *executionResolver) sourceDeclarations() []action.Library {
	r.mu.Lock()
	defer r.mu.Unlock()
	byOwner := make(map[string]*action.Library)
	names := make([]string, 0, len(r.owners))
	for name, owner := range r.owners {
		if owner.library != baseResolverOwner {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var owners []string
	for _, name := range names {
		owner := r.owners[name].library
		lib := byOwner[owner]
		if lib == nil {
			lib = &action.Library{Name: owner}
			byOwner[owner] = lib
			owners = append(owners, owner)
		}
		if act, ok := r.mountedActions[name]; ok {
			lib.Actions = append(lib.Actions, act)
		}
		if src, ok := r.mountedStreams[name]; ok {
			lib.Sources = append(lib.Sources, src)
		}
		if op, ok := r.mountedOperators[name]; ok {
			lib.Operators = append(lib.Operators, op)
		}
	}
	libraries := make([]action.Library, 0, len(owners))
	for _, owner := range owners {
		libraries = append(libraries, *byOwner[owner])
	}
	return libraries
}
