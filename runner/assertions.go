package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/expr-lang/expr"

	compiler "github.com/nexssp/flow/compiler"
)

const (
	assertRed   = "\x1b[31m"
	assertGreen = "\x1b[32m"
	assertReset = "\x1b[0m"
)

// assertColorEnabled mirrors cli.colorEnabled without importing cli.
// Color is enabled only when w is a terminal-backed *os.File.
func assertColorEnabled(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func paintAssert(s, color string, enabled bool) string {
	if !enabled {
		return s
	}
	return color + s + assertReset
}

// RunAssertions evaluates every @assert expression against the run
// result and prints a per-assert pass/fail line.
//
// Failures are tinted red when the output writer is a terminal. When
// at least one assert fails, a trailing "exit status: N" line reports
// the process exit code the CLI is about to return, so a caller
// scanning the tail of the output sees the outcome without inferring
// it from the individual ✗ marks.
func RunAssertions(w io.Writer, result any, durationMS int64, actions, asserts []string, verbosity int, localSymbols ...map[string]compiler.SymbolNamespace) int {
	return RunAssertionsContext(context.Background(), w, result, durationMS, actions, asserts, verbosity, localSymbols...)
}

// RunAssertionsContext evaluates every @assert expression using the caller's execution context.
func RunAssertionsContext(ctx context.Context, w io.Writer, result any, durationMS int64, actions, asserts []string, verbosity int, localSymbols ...map[string]compiler.SymbolNamespace) int {
	return RunAssertionsContextWithPositions(ctx, w, result, durationMS, actions, asserts, nil, verbosity, localSymbols...)
}

// RunAssertionsContextWithPositions evaluates assertions and includes the
// matching source position in each failure when one is available. Positions
// are aligned by assertion index; zero positions are treated as unknown.
func RunAssertionsContextWithPositions(ctx context.Context, w io.Writer, result any, durationMS int64, actions, asserts []string, positions []compiler.Position, verbosity int, localSymbols ...map[string]compiler.SymbolNamespace) int {
	if len(asserts) == 0 {
		return 0
	}

	color := assertColorEnabled(w)

	env := assertionEnvironment(ctx, result, durationMS, actions, firstSymbolNamespace(localSymbols))

	if verbosity >= 1 {
		_, _ = fmt.Fprintf(w, "\n🧪 ASSERTIONS (%d)\n", len(asserts))
	}

	failed := 0
	for i, src := range asserts {
		location := assertionSourcePrefix(positions, i)
		out, phase, err := evaluateAssertion(src, env)
		if err != nil {
			if phase == "runtime" {
				_, _ = fmt.Fprintf(w, "  %s %s%s  (runtime: %v)\n",
					paintAssert("✗", assertRed, color), location, src, err)
			} else {
				_, _ = fmt.Fprintf(w, "  %s %s%s  (syntax: %v)\n",
					paintAssert("✗", assertRed, color), location, src, err)
			}
			failed++
			continue
		}
		if b, ok := out.(bool); ok && b {
			if verbosity >= 1 {
				_, _ = fmt.Fprintf(w, "  %s %s\n",
					paintAssert("✓", assertGreen, color), src)
			}
			continue
		}
		_, _ = fmt.Fprintf(w, "  %s %s%s  (got %v)\n",
			paintAssert("✗", assertRed, color), location, src, out)
		failed++
	}

	if failed > 0 {
		_, _ = fmt.Fprintf(w, "  %s\n",
			paintAssert(fmt.Sprintf("exit status: %d", 1), assertRed, color))
		return 1
	}

	if verbosity >= 1 {
		_, _ = fmt.Fprintf(w, "  %s\n",
			paintAssert("✓ all assertions passed", assertGreen, color))
	}
	return 0
}

// EvaluateAssertions evaluates assertions without rendering CLI output. It is
// shared with self-test so both paths use the same expression and symbol rules.
func EvaluateAssertions(result any, asserts []string, localSymbols ...map[string]compiler.SymbolNamespace) []error {
	return EvaluateAssertionsContext(context.Background(), result, asserts, localSymbols...)
}

// EvaluateAssertionsContext evaluates assertions without rendering output and preserves the caller's context.
func EvaluateAssertionsContext(ctx context.Context, result any, asserts []string, localSymbols ...map[string]compiler.SymbolNamespace) []error {
	return EvaluateAssertionsContextWithPositions(ctx, result, asserts, nil, localSymbols...)
}

// EvaluateAssertionsContextWithPositions evaluates assertions without
// rendering output and prefixes failures with their source position when known.
func EvaluateAssertionsContextWithPositions(ctx context.Context, result any, asserts []string, positions []compiler.Position, localSymbols ...map[string]compiler.SymbolNamespace) []error {
	if len(asserts) == 0 {
		return nil
	}
	env := assertionEnvironment(ctx, result, 0, nil, firstSymbolNamespace(localSymbols))
	var failures []error
	for i, src := range asserts {
		location := assertionSourcePrefix(positions, i)
		out, phase, err := evaluateAssertion(src, env)
		if err != nil {
			failures = append(failures, fmt.Errorf("%sassert %q %s: %w", location, src, phase, err))
			continue
		}
		passed, ok := out.(bool)
		if !ok || !passed {
			failures = append(failures, fmt.Errorf("%sassert %q failed (got %v)", location, src, out))
		}
	}
	return failures
}

func assertionSourcePrefix(positions []compiler.Position, index int) string {
	if index >= len(positions) {
		return ""
	}
	position := positions[index]
	if position.File == "" || position.Line <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d: ", position.File, position.Line)
}

func assertionEnvironment(ctx context.Context, result any, durationMS int64, actions []string, localSymbols map[string]compiler.SymbolNamespace) map[string]any {
	normalized := JSONMapView(result)
	env := map[string]any{
		"result":      normalized,
		"__root__":    normalized,
		"duration_ms": durationMS,
		"actions":     actions,
	}
	if normalizedMap, ok := normalized.(map[string]any); ok {
		maps.Copy(env, normalizedMap)
	}
	return compiler.MergeSymbolNamespaces(compiler.WithLocalSymbols(ctx, localSymbols), env)
}

func firstSymbolNamespace(namespaces []map[string]compiler.SymbolNamespace) map[string]compiler.SymbolNamespace {
	if len(namespaces) == 0 {
		return nil
	}
	return namespaces[0]
}

func evaluateAssertion(src string, env map[string]any) (value any, phase string, err error) {
	trimmed := strings.TrimSpace(compiler.StripExprComments(src))
	cleanExpr := trimmed
	if strings.HasPrefix(trimmed, ".") {
		cleanExpr = compiler.PreprocessDots(trimmed)
	}
	program, err := expr.Compile(cleanExpr, expr.Env(env))
	if err != nil {
		return nil, "syntax", err
	}
	output, err := expr.Run(program, env)
	if err != nil {
		return nil, "runtime", err
	}
	return output, "", nil
}

// JSONMapView applies JSON tags to struct outputs before expr evaluation.
func JSONMapView(value any) any {
	if value == nil {
		return nil
	}
	if _, ok := value.(map[string]any); ok {
		return value
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return value
	}
	return normalized
}
