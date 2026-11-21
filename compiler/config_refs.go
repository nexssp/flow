package compiler

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nexssp/flow/internal/lexical"
)

// CompileTimeExpressionRewriter lets extension-owned AST nodes expose their
// raw expression source to compile-time reference passes.
type CompileTimeExpressionRewriter interface {
	RewriteCompileTimeExpressions(func(string, Position) (string, error)) error
}

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
	return validateAllFlagAndArgRefs(ast, cliArgs)
}

// resolveConfigRefsInExpressions substitutes @config.KEY references in
// expression source before extension validation, type analysis, or building.
func resolveConfigRefsInExpressions(ast Expr, cfg map[string]string) error {
	if ast == nil {
		return nil
	}
	rewrite := func(source string, pos Position) (string, error) {
		return substituteConfigRefsInExpression(source, pos, cfg)
	}
	var firstErr error
	walkExpr(ast, func(node Expr) {
		if firstErr != nil {
			return
		}
		switch current := node.(type) {
		case *ValueExpr:
			firstErr = resolveConfigExpressionValue(current.Value, current.Pos, rewrite)
		case *Atom:
			firstErr = resolveConfigExpressionValue(current.DirectInput, current.Pos, rewrite)
			if firstErr != nil {
				return
			}
			keys := make([]string, 0, len(current.Args))
			for key := range current.Args {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				if firstErr = resolveConfigExpressionValue(current.Args[key], current.Pos, rewrite); firstErr != nil {
					return
				}
			}
			for _, value := range current.ArgSpreads {
				if firstErr = resolveConfigExpressionValue(value, current.Pos, rewrite); firstErr != nil {
					return
				}
			}
		case *ProjectionExpr:
			current.Raw, firstErr = substituteConfigRefsInProjectionExpression(current.Raw, current.Pos, cfg)
		case *LoopExpr:
			current.Until, firstErr = rewrite(current.Until, current.Pos)
		case *AssertExpr:
			current.Condition, firstErr = rewrite(current.Condition, current.Pos)
		}
		if firstErr != nil {
			return
		}
		if extension, ok := node.(CompileTimeExpressionRewriter); ok {
			firstErr = extension.RewriteCompileTimeExpressions(rewrite)
		}
	})
	return firstErr
}

func resolveConfigExpressionValue(value *Value, pos Position, rewrite func(string, Position) (string, error)) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case ValueExpression:
		resolved, err := rewrite(value.Expression, pos)
		if err != nil {
			return err
		}
		if resolved != value.Expression {
			value.Expression = resolved
			value.Program = nil
		}
	case ValueMap:
		for _, entry := range value.Map {
			if err := resolveConfigExpressionValue(entry.Value, pos, rewrite); err != nil {
				return err
			}
		}
	case ValueSlice:
		for _, item := range value.Slice {
			if err := resolveConfigExpressionValue(item, pos, rewrite); err != nil {
				return err
			}
		}
	case ValueString, ValueBare, ValueNumber, ValueBool, ValueNull, ValueRef, ValueTyped:
		// Scalar values contain no expression source to rewrite.
	}
	return nil
}

func substituteConfigRefsInExpression(source string, pos Position, cfg map[string]string) (string, error) {
	return substituteConfigRefsInExpressionWithProjectionComments(source, pos, cfg, false)
}

func substituteConfigRefsInProjectionExpression(source string, pos Position, cfg map[string]string) (string, error) {
	return substituteConfigRefsInExpressionWithProjectionComments(source, pos, cfg, true)
}

func substituteConfigRefsInExpressionWithProjectionComments(source string, pos Position, cfg map[string]string, skipProjectionComments bool) (string, error) {
	if !strings.Contains(source, "@config.") {
		return source, nil
	}
	var out strings.Builder
	out.Grow(len(source))
	parenDepth := 0
	for i := 0; i < len(source); {
		if source[i] == '"' || source[i] == '\'' || source[i] == '`' {
			end := configExpressionStringEnd(source, i)
			if source[i] != '`' && end > i+1 {
				if replacement, ok, err := substituteQuotedConfigReference(source[i:end], pos, cfg); err != nil {
					return "", err
				} else if ok {
					out.WriteString(replacement)
					i = end
					continue
				}
			}
			out.WriteString(source[i:end])
			i = end
			continue
		}
		if end, ok := configExpressionCommentEnd(source, i, parenDepth, skipProjectionComments); ok {
			out.WriteString(source[i:end])
			i = end
			continue
		}
		if replacement, next, matched, err := resolveConfigReferenceAt(source, i, pos, cfg); err != nil {
			return "", err
		} else if matched {
			out.WriteString(replacement)
			i = next
			continue
		}
		switch source[i] {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String(), nil
}

func configExpressionCommentEnd(source string, start, parenDepth int, skipProjectionComments bool) (int, bool) {
	if start+1 < len(source) && source[start] == '/' {
		switch source[start+1] {
		case '/':
			end := strings.IndexByte(source[start+2:], '\n')
			if end < 0 {
				return len(source), true
			}
			return start + 2 + end, true
		case '*':
			return configExpressionBlockCommentEnd(source, start), true
		}
	}
	if skipProjectionComments && source[start] == '#' && (parenDepth == 0 || lexical.IsHashLineComment(source, start)) {
		end := strings.IndexAny(source[start:], "\r\n")
		if end < 0 {
			return len(source), true
		}
		return start + end, true
	}
	return start, false
}

func resolveConfigReferenceAt(source string, index int, pos Position, cfg map[string]string) (replacement string, next int, matched bool, err error) {
	if !strings.HasPrefix(source[index:], "@config.") {
		return "", index, false, nil
	}
	start := index + len("@config.")
	end := configReferenceKeyEnd(source, start)
	if end == start {
		return "", index, false, nil
	}
	key := source[start:end]
	value, ok := cfg[key]
	if !ok {
		return "", index, false, SourceError(pos,
			"expression: unknown @config.%s (declared: %s)", key, declaredConfigKeys(cfg))
	}
	return strconv.Quote(value), end, true, nil
}

func substituteQuotedConfigReference(literal string, pos Position, cfg map[string]string) (replacement string, matched bool, err error) {
	if len(literal) < 2 {
		return "", false, nil
	}
	content := literal[1 : len(literal)-1]
	const prefix = "@config."
	if !strings.HasPrefix(content, prefix) {
		return "", false, nil
	}
	keyStart := len(prefix)
	keyEnd := configReferenceKeyEnd(content, keyStart)
	if keyEnd != len(content) {
		return "", false, nil
	}
	key := content[keyStart:keyEnd]
	value, ok := cfg[key]
	if !ok {
		return "", false, SourceError(pos,
			"expression: unknown @config.%s (declared: %s)", key, declaredConfigKeys(cfg))
	}
	return strconv.Quote(value), true, nil
}

func configExpressionStringEnd(source string, start int) int {
	quote := source[start]
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' && quote != '`' && i+1 < len(source) {
			i++
			continue
		}
		if source[i] == quote {
			return i + 1
		}
	}
	return len(source)
}

func configExpressionBlockCommentEnd(source string, start int) int {
	depth := 1
	for i := start + 2; i < len(source); i++ {
		if i+1 < len(source) && source[i:i+2] == "/*" {
			depth++
			i++
			continue
		}
		if i+1 < len(source) && source[i:i+2] == "*/" {
			depth--
			i++
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(source)
}

func configReferenceKeyEnd(source string, start int) int {
	for i := start; i < len(source); {
		r, size := utf8.DecodeRuneInString(source[i:])
		if r != '_' && r != '.' && r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		i += size
		start = i
	}
	return start
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
		firstErr = walkExprValues(node, func(atom *Atom, raw, where string) error {
			return checkConfigRef(atom, raw, where, cfg)
		})
	})
	return firstErr
}

func validateAllFlagAndArgRefs(ast Expr, cliArgs []string) error {
	knownFlags := make(map[string]struct{})
	var positional []string
	for _, arg := range cliArgs {
		if name, ok := flagNameFromArg(arg); ok {
			knownFlags[name] = struct{}{}
		} else if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
		}
	}
	var firstErr error
	walkExpr(ast, func(node Expr) {
		if firstErr != nil {
			return
		}
		firstErr = walkExprValues(node, func(atom *Atom, raw, where string) error {
			if err := checkFlagRef(atom, raw, where, knownFlags); err != nil {
				return err
			}
			return checkArgRef(atom, raw, where, len(positional))
		})
	})
	return firstErr
}

func checkConfigRef(atom *Atom, raw, where string, cfg map[string]string) error {
	expr, fallback, hasFallback := strings.Cut(raw, "??")
	if hasFallback && strings.TrimSpace(fallback) != "" {
		return nil
	}
	key, ok := strings.CutPrefix(strings.TrimSpace(expr), "@config.")
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
	expr, fallback, hasFallback := strings.Cut(raw, "??")
	if hasFallback && strings.TrimSpace(fallback) != "" {
		return nil
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(expr), "@flag.")
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

func checkArgRef(atom *Atom, raw, where string, positionalCount int) error {
	expr, fallback, hasFallback := strings.Cut(raw, "??")
	if hasFallback && strings.TrimSpace(fallback) != "" {
		return nil
	}
	idxStr, ok := strings.CutPrefix(strings.TrimSpace(expr), "@arg.")
	if !ok {
		return nil
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 1 {
		return SourceError(atom.Pos, "%s: invalid positional argument index %q (use @arg.1, @arg.2, ...)", where, idxStr)
	}
	if idx > positionalCount {
		return SourceError(atom.Pos,
			"%s: missing required positional argument %d (received %d)"+
				"\n  hint: pass argument %d on the command line (e.g. nflow <file.nflow> <arg%d>)",
			where, idx, positionalCount, idx, idx)
	}
	return nil
}

// validateAllFlagRefs is a flag-only variant of the @flag check: it
// walks the AST and validates every @flag.NAME reference against the
// names present in cliArgs, without performing the @arg.N arity check
// that validateAllFlagAndArgRefs layers on top.
//
// It is retained because the two checks are conceptually separable —
// a caller that has already decided arg references are not its concern
// (for example, a future lint mode that inspects positional-argument
// usage independently from flags) needs the flag pass on its own. The
// current compile path uses validateAllFlagAndArgRefs because it wants
// both diagnostics in a single walk.
//
//nolint:unused // Reserved for the flag-only validation mode; the compile path currently uses validateAllFlagAndArgRefs.
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
// parenthesized params, named arguments (recursively), and modifier
// values. It never descends into Value kinds that cannot hold a textual
// reference (numbers, bools, state references).
func walkAtomValues(atom *Atom, visit func(raw, where string) error) error {
	paramKeys := make([]string, 0, len(atom.Params))
	for key := range atom.Params {
		paramKeys = append(paramKeys, key)
	}
	slices.Sort(paramKeys)
	for _, key := range paramKeys {
		value := atom.Params[key]
		if err := visit(value, "param "+key); err != nil {
			return err
		}
	}
	argKeys := make([]string, 0, len(atom.Args))
	for key := range atom.Args {
		argKeys = append(argKeys, key)
	}
	slices.Sort(argKeys)
	for _, key := range argKeys {
		val := atom.Args[key]
		if err := walkValueStrings(val, "arg "+key, visit); err != nil {
			return err
		}
	}
	for i, value := range atom.ArgSpreads {
		if err := walkValueStrings(value, "argument-map spread "+strconv.Itoa(i+1), visit); err != nil {
			return err
		}
	}
	return walkModifierValues(atom.Modifiers, visit)
}

func walkExprValues(expr Expr, visit func(atom *Atom, raw, where string) error) error {
	switch node := expr.(type) {
	case *ValueExpr:
		atom := &Atom{Pos: node.Pos}
		return walkValueStrings(node.Value, "value", func(raw, where string) error {
			return visit(atom, raw, where)
		})
	case *Atom:
		return walkAtomValues(node, func(raw, where string) error {
			return visit(node, raw, where)
		})
	case *DecoratedExpr:
		atom := &Atom{Pos: node.Pos}
		return walkModifierValues(node.Modifiers, func(raw, where string) error {
			return visit(atom, raw, where)
		})
	default:
		return nil
	}
}

func walkModifierValues(modifiers []string, visit func(raw, where string) error) error {
	for _, raw := range modifiers {
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

func resolveConfigRefsInModifiers(cfg map[string]string, cliArgs, modifiers []string) []string {
	resolved := make([]string, len(modifiers))
	for i, raw := range modifiers {
		resolved[i] = resolveConfigRefsInModifier(cfg, cliArgs, raw)
	}
	return resolved
}

func walkValueStrings(v *Value, where string, visit func(raw, where string) error) error {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case ValueString:
		return visit(v.Str, where)
	case ValueRef:
		if strings.HasPrefix(v.Ref, "config.") ||
			strings.HasPrefix(v.Ref, "flag.") ||
			strings.HasPrefix(v.Ref, "arg.") {
			return visit("@"+v.Ref, where)
		}
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
	case ValueBare, ValueNumber, ValueBool, ValueNull, ValueTyped, ValueExpression:
	}
	return nil
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
