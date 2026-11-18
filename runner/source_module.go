package runner

import (
	"context"
	"fmt"
	"sort"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
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
	type importedContract struct {
		name     string
		contract compiler.CapabilityContract
	}
	var contracts []importedContract
	// Exports already contain compiled actions with their hooks. Do not attach
	// those hooks again at each namespace boundary of a nested import.
	root.mu.Lock()
	for i := range libraries {
		lib := &libraries[i]
		lib.Name = fmt.Sprintf("%s AS %s (%s)", lib.Name, imported.Namespace, imported.Position)
		qualified := compiler.QualifySourceLibrary(*lib, imported.Namespace, bind)
		for actionIndex, sourceAction := range lib.Actions {
			if sourceAction == nil || sourceAction.Describe() == nil || actionIndex >= len(qualified.Actions) {
				continue
			}
			contract, found := local.Contract(sourceAction.Describe().Name)
			if !found || !contract.SecretBoundary && !secret.TypeContains(contract.Input.Type) && !secret.TypeContains(contract.Output.Type) {
				continue
			}
			contractName := qualified.Actions[actionIndex].Describe().Name
			contract.Name = contractName
			contracts = append(contracts, importedContract{name: contractName, contract: contract})
		}
		if err := root.mountLocked(qualified, "", nil); err != nil {
			root.mu.Unlock()
			return compiler.SourceError(imported.Position, "@include: %w", err)
		}
	}
	root.invalidateAllSnapshotsLocked()
	root.mu.Unlock()
	registrar, ok := parent.(compiler.ContractRegistrar)
	if len(contracts) > 0 && !ok {
		return compiler.SourceError(imported.Position, "@include: parent source resolver cannot preserve Secret-typed pipeline contracts")
	}
	for _, contract := range contracts {
		if err := registrar.RegisterContract(contract.name, contract.contract); err != nil {
			return compiler.SourceError(imported.Position, "@include: register Secret-typed contract for %q: %w", contract.name, err)
		}
	}
	return nil
}

func sourceName(prepared *compiler.PreparedSource) string {
	return prepared.SourceName()
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
