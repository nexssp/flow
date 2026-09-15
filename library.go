package flow

import (
	"fmt"
	"log/slog"

	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

// Alias associates a canonical action name with one or more short names
// that .flow authors may use in its place.
//
// Aliases are a slice, not a map: duplicate declarations are errors,
// not silent last-wins.
type Alias struct {
	Canonical string
	Short     []string
}

// Library is a named bag of actions contributed by one package.
//
// Library is a value type, not an interface: the fields are the whole
// contract, and the runtime only ever reads them.
//
// Overrides lists canonical action names that this library
// intentionally replaces from an earlier library in the same
// BuildRegistry call. Any collision not listed here is a hard error.
type Library struct {
	Name        string
	Description string
	Actions     []action.AnyAction
	Hooks       []action.AnyHook
	Aliases     []Alias
	Overrides   []string
}

// StandardLibrary returns the flow package's own action set: logging,
// benchmarking, distribution, and supervision.
func StandardLibrary() Library {
	return Library{
		Name:        "flow",
		Description: "Standard flow actions: log, bench, distribute, supervisor",
		Actions:     nodes.All(),
		Aliases: []Alias{
			{Canonical: "log.info", Short: []string{"log", "info"}},
			{Canonical: "log.warn", Short: []string{"warn"}},
			{Canonical: "log.error", Short: []string{"error"}},

			{Canonical: "bench.run", Short: []string{"bench", "benchmark"}},
			{Canonical: "bench.save", Short: []string{"save_bench"}},
			{Canonical: "bench.compare", Short: []string{"compare", "diff"}},

			{Canonical: "distribute.map", Short: []string{"map", "fanout", "parallel"}},
			{Canonical: "distribute.reduce", Short: []string{"reduce", "fold"}},
		},
	}
}

// BuildRegistry creates a MapRegistry from a set of libraries.
//
// Rules:
//  1. Every primary action is registered under its canonical name.
//  2. When two libraries declare the same canonical name, the later
//     library MUST list that name in its Overrides. Otherwise
//     BuildRegistry returns an error naming both libraries.
//  3. Hooks from every library are applied to every surviving action.
//  4. Aliases are registered last, so a canonical name always wins
//     over an alias, and an earlier library always wins over a later
//     one on alias collisions.
func BuildRegistry(libs ...Library) (*MapRegistry, error) {
	// Validate Overrides and Alias tables per library up front.
	allowed := make(map[string]string, 16)

	for i := range libs {
		lib := &libs[i]
		for _, name := range lib.Overrides {
			if prev, dup := allowed[name]; dup {
				return nil, fmt.Errorf(
					"flow: %q and %q both declare Overrides for %q",
					prev, lib.Name, name)
			}

			allowed[name] = lib.Name
		}

		seenAlias := make(map[string]bool, len(lib.Aliases))
		for _, a := range lib.Aliases {
			if a.Canonical == "" {
				return nil, fmt.Errorf(
					"flow: library %q has an alias with no canonical name", lib.Name)
			}

			if seenAlias[a.Canonical] {
				return nil, fmt.Errorf(
					"flow: library %q declares aliases for %q twice",
					lib.Name, a.Canonical)
			}

			seenAlias[a.Canonical] = true
		}
	}

	type claim struct {
		libName string
		action  action.AnyAction
	}

	winner := make(map[string]claim, 64)

	for i := range libs {
		lib := &libs[i]

		seenInThisLib := make(map[string]bool, len(lib.Actions))
		for _, act := range lib.Actions {
			if act == nil {
				continue
			}

			meta := act.Describe()
			if meta == nil || meta.Name == "" {
				continue
			}

			if seenInThisLib[meta.Name] {
				return nil, fmt.Errorf(
					"flow: library %q declares %q more than once",
					lib.Name, meta.Name)
			}

			seenInThisLib[meta.Name] = true

			if prev, dup := winner[meta.Name]; dup {
				if allowed[meta.Name] != lib.Name {
					return nil, fmt.Errorf(
						"flow: action %q declared by both %q and %q; "+
							"if the override is intentional, add %q to %s.Overrides",
						meta.Name, prev.libName, lib.Name, meta.Name, lib.Name)
				}

				slog.Warn("flow: action overridden",
					"action", meta.Name,
					"previous", prev.libName,
					"current", lib.Name)
			}

			winner[meta.Name] = claim{libName: lib.Name, action: act}
		}
	}

	var allHooks []action.AnyHook

	for i := range libs {
		lib := &libs[i]
		allHooks = append(allHooks, lib.Hooks...)
	}

	reg := NewRegistry()

	for name, c := range winner {
		if len(allHooks) > 0 {
			c.action.AddAnyHook(allHooks...)
		}

		reg.Register(name, c.action)
	}

	for i := range libs {
		lib := &libs[i]
		for _, a := range lib.Aliases {
			act, ok := reg.Get(a.Canonical)
			if !ok {
				continue
			}

			for _, short := range a.Short {
				if _, exists := reg.Get(short); exists {
					continue
				}

				reg.Register(short, act)
			}
		}
	}

	return reg, nil
}

// LibraryNames returns the names of the given libraries, in order.
func LibraryNames(libs []Library) []string {
	out := make([]string, 0, len(libs))
	for i := range libs {
		lib := &libs[i]
		if lib.Name != "" {
			out = append(out, lib.Name)
		}
	}

	return out
}
