package match

import (
	"maps"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

// ConditionCase is one ordered expression condition, or a default branch.
// A nil Program is skipped unless IsDefault is true.
type ConditionCase struct {
	Program   *vm.Program
	IsDefault bool
}

// CompileCondition compiles an expression using Flow's input-dot syntax.
func CompileCondition(source string) (*vm.Program, error) {
	return expr.Compile(core.PreprocessDots(source), expr.AllowUndefinedVariables())
}

// EvaluateProgram runs a compiled condition or subject expression.
func EvaluateProgram(program *vm.Program, env map[string]any) (any, error) {
	return expr.Run(program, env)
}

// FirstMatchingCase returns the first condition that evaluates to true, or
// the first default case reached in order. Evaluation failures are skipped,
// matching the existing match and @on_error behavior.
func FirstMatchingCase(cases []ConditionCase, env map[string]any) int {
	for i, current := range cases {
		if current.IsDefault {
			return i
		}
		if current.Program == nil {
			continue
		}
		value, err := EvaluateProgram(current.Program, env)
		if err == nil && isTruthy(value) {
			return i
		}
	}
	return -1
}

// ResolveKindSymbol maps a symbolic Flow name to one of the built-in Kernel
// kinds. It intentionally does not treat xerr.Kind as a closed runtime set:
// custom values remain possible and are handled by explicit else cases.
func ResolveKindSymbol(symbol string) (xerr.Kind, bool) {
	for _, kind := range xerr.AllKinds() {
		if symbol == KindSymbol(kind) {
			return kind, true
		}
	}
	return "", false
}

// KindSymbol returns the source spelling of a built-in Kernel kind.
func KindSymbol(kind xerr.Kind) string {
	return "xerr.Kind" + string(kind)
}

// KindEnvironment returns fresh bindings for symbolic Kernel kinds.
func KindEnvironment() map[string]any {
	return KindEnvironmentFor(nil)
}

// KindEnvironmentFor returns symbolic bindings with the same runtime
// representation as kindValue. Legacy @on_error exposes error.kind as a
// string; expression guards expose it as xerr.Kind.
func KindEnvironmentFor(kindValue any) map[string]any {
	symbols := make(map[string]any, len(xerr.AllKinds()))
	_, stringKind := kindValue.(string)
	for _, kind := range xerr.AllKinds() {
		if stringKind {
			symbols["Kind"+string(kind)] = string(kind)
		} else {
			symbols["Kind"+string(kind)] = kind
		}
	}
	return map[string]any{"xerr": symbols}
}

// WithKindEnvironment returns a fresh expression environment with built-in
// Kernel kind symbols added under xerr. The caller's map is never mutated.
func WithKindEnvironment(env map[string]any) map[string]any {
	out := make(map[string]any, len(env)+1)
	maps.Copy(out, env)
	kindValue, hasKind := errorKindValue(out)
	if hasKind {
		out["xerr"] = KindEnvironmentFor(kindValue)["xerr"]
	} else {
		out["xerr"] = KindEnvironment()["xerr"]
	}
	return out
}

func errorKindValue(env map[string]any) (any, bool) {
	if info, ok := env["error"].(map[string]any); ok {
		if value, found := info["kind"]; found {
			return value, true
		}
	}
	value, found := env["kind"]
	return value, found
}

func isTruthy(value any) bool {
	b, ok := value.(bool)
	return ok && b
}
