package core

import (
	"slices"
	"strconv"
	"strings"
)

// validateConfigRefs walks the AST and fails on the first @config.KEY
// reference whose KEY was not declared via a @config directive, and the
// first @flag.NAME reference whose NAME was not passed on the command
// line. Both checks are opt-in per source: @config validation runs only
// when the source declares at least one @config pair, and @flag
// validation runs only when the compile context carries a non-nil
// CLI-args slice. Sources with neither skip both passes, so embedded
// runs and pure-compile tests are unaffected.
func validateConfigRefs(ast Expr, cfg map[string]string, cliArgs []string) error {
	if ast == nil {
		return nil
	}
	if err := validateAllConfigRefs(ast, cfg); err != nil {
		return err
	}
	return validateAllFlagRefs(ast, cliArgs)
}

func validateAllConfigRefs(ast Expr, cfg map[string]string) error {
	if len(cfg) == 0 {
		return nil
	}
	var firstErr error
	walkExpr(ast, func(node Expr) {
		if firstErr != nil {
			return
		}
		atom, ok := node.(*Atom)
		if !ok {
			return
		}
		firstErr = walkAtomValues(atom, func(raw, where string) error {
			return checkConfigRef(atom, raw, where, cfg)
		})
	})
	return firstErr
}

func validateAllFlagRefs(ast Expr, cliArgs []string) error {
	if cliArgs == nil {
		return nil
	}
	known := make(map[string]struct{}, len(cliArgs))
	for _, arg := range cliArgs {
		if name, ok := flagNameFromArg(arg); ok {
			known[name] = struct{}{}
		}
	}
	var firstErr error
	walkExpr(ast, func(node Expr) {
		if firstErr != nil {
			return
		}
		atom, ok := node.(*Atom)
		if !ok {
			return
		}
		firstErr = walkAtomValues(atom, func(raw, where string) error {
			return checkFlagRef(atom, raw, where, known)
		})
	})
	return firstErr
}

// walkAtomValues visits every string-carrying position on one atom:
// legacy parenthesised params, @{...} args (recursively), and modifier
// values. It never descends into Value kinds that cannot hold a textual
// reference (numbers, bools, state references).
func walkAtomValues(atom *Atom, visit func(raw, where string) error) error {
	for key, value := range atom.Params {
		if err := visit(value, "param "+key); err != nil {
			return err
		}
	}
	for key, val := range atom.Args {
		if err := walkValueStrings(val, "arg "+key, visit); err != nil {
			return err
		}
	}
	for _, raw := range atom.Modifiers {
		name, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		if err := visit(value, "modifier :"+name); err != nil {
			return err
		}
	}
	return nil
}

func walkValueStrings(v *Value, where string, visit func(raw, where string) error) error {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case ValueString:
		return visit(v.Str, where)
	case ValueMap:
		for _, entry := range v.Map {
			if err := walkValueStrings(entry.Value, where+"."+entry.Key, visit); err != nil {
				return err
			}
		}
	case ValueSlice:
		for i, item := range v.Slice {
			if err := walkValueStrings(item, where+"["+strconv.Itoa(i)+"]", visit); err != nil {
				return err
			}
		}
	case ValueBare, ValueNumber, ValueBool, ValueNull, ValueRef:
	}
	return nil
}

func checkConfigRef(atom *Atom, raw, where string, cfg map[string]string) error {
	key, ok := strings.CutPrefix(raw, "@config.")
	if !ok {
		return nil
	}
	if _, exists := cfg[key]; exists {
		return nil
	}
	return SourceError(atom.Pos,
		"%s: unknown @config.%s (declared: %s)",
		where, key, declaredConfigKeys(cfg))
}

func checkFlagRef(atom *Atom, raw, where string, known map[string]struct{}) error {
	name, ok := strings.CutPrefix(raw, "@flag.")
	if !ok {
		return nil
	}
	if _, exists := known[name]; exists {
		return nil
	}
	return SourceError(atom.Pos,
		"%s: unknown @flag.%s (available: %s)"+
			"\n  hint: pass --%s=VALUE on the command line",
		where, name, availableFlagNames(known), name)
}

// flagNameFromArg extracts the flag name from a "-x=y" or "--x=y"
// argument. Returns ("", false) for arguments that do not carry the
// required "=VALUE" suffix, matching the resolution rule in
// resolveConfigString.
func flagNameFromArg(arg string) (string, bool) {
	name, _, ok := strings.Cut(arg, "=")
	if !ok {
		return "", false
	}
	name = strings.TrimPrefix(name, "--")
	name = strings.TrimPrefix(name, "-")
	if name == "" {
		return "", false
	}
	return name, true
}

func declaredConfigKeys(cfg map[string]string) string {
	keys := make([]string, 0, len(cfg))
	for key := range cfg {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, ", ")
}

func availableFlagNames(known map[string]struct{}) string {
	if len(known) == 0 {
		return "none"
	}
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
