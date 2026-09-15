// Assembly is the single composition pipeline for a nexss binary. Every
// capability on App is a loader: a function that receives the accumulating
// action set and appends to it. Loaders run in declaration order, which
// makes override and shadowing rules explicit.
//
// The four rules that make this maintainable:
//
//  1. Order is deliberate. Prompts and skills load before flows, so a .flow
//     file can reference prompt.summarize by name.
//  2. Last-wins. If two sources produce the same action name, the later
//     source replaces the earlier one. User templates shadow builtins.
//  3. Every With* has an escape hatch. If a loader is not what you want,
//     use WithActions and build the slice yourself.
//  4. Nothing is hidden. Callers see the final action list via App.Actions
//     before Run() starts the transport.
package bootstrap

import (
	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

// loader is the internal shape of a capability stage. It receives the live
// assembly and may append actions, register named providers, or do nothing.
type loader func(*Assembly) error

// Assembly is the running set of actions and shared dependencies built
// during Run(). Loaders mutate it, but the final slice is frozen once the
// server starts.
type Assembly struct {
	// Actions in load order. Later entries replace earlier entries with the
	// same name via dedupe-by-name during finalize().
	Actions []action.AnyAction

	// registry is built incrementally so flow files can reference actions
	// by name during their compile step.
	registry *flow.MapRegistry
}

func newAssembly() *Assembly {
	return &Assembly{registry: flow.NewRegistry()}
}

// finalize dedupes by action name (last-wins), registers the surviving
// set, appends any flow-standard primitives the caller did not supply,
// and returns the final action slice. Called once, immediately before
// transport mounting.
func (a *Assembly) finalize() []action.AnyAction {
	// 1. Dedup by action name, last-wins.
	lastIdx := make(map[string]int, len(a.Actions))
	for i, act := range a.Actions {
		if act == nil || act.Describe() == nil {
			continue
		}

		lastIdx[act.Describe().Name] = i
	}

	out := make([]action.AnyAction, 0, len(lastIdx))

	for i, act := range a.Actions {
		if act == nil || act.Describe() == nil {
			continue
		}

		if lastIdx[act.Describe().Name] != i {
			continue
		}

		out = append(out, act)
	}

	// 2. Register the surviving set into the shared registry.
	for _, act := range out {
		a.registry.Register(act.Describe().Name, act)
	}

	// 3. Flow standard library: log.*, bench.*, distribute.*, supervisor.
	//    flow.StandardLibrary() is the single source of truth; we append
	//    only the actions the caller has not already provided.
	core := flow.StandardLibrary().Actions
	for _, act := range core {
		meta := act.Describe()
		if meta == nil {
			continue
		}

		if _, exists := a.registry.Get(meta.Name); exists {
			continue
		}

		a.registry.Register(meta.Name, act)
		out = append(out, act)
	}

	// 4. Standard aliases for the flow primitives. Registered last so
	//    user-provided actions always win.
	for _, alias := range flow.StandardLibrary().Aliases {
		act, ok := a.registry.Get(alias.Canonical)
		if !ok {
			continue
		}

		for _, short := range alias.Short {
			if _, exists := a.registry.Get(short); exists {
				continue
			}

			a.registry.Register(short, act)
		}
	}

	return out
}
