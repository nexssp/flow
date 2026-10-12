package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/expr-lang/expr"

	core "github.com/nexssp/flow/compiler"
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
func RunAssertions(w io.Writer, result any, durationMS int64, actions, asserts []string, verbosity int) int {
	if len(asserts) == 0 {
		return 0
	}

	color := assertColorEnabled(w)

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

	if verbosity >= 1 {
		_, _ = fmt.Fprintf(w, "\n🧪 ASSERTIONS (%d)\n", len(asserts))
	}

	failed := 0
	for _, src := range asserts {
		trimmed := strings.TrimSpace(core.StripExprComments(src))
		cleanExpr := trimmed
		if strings.HasPrefix(trimmed, ".") {
			cleanExpr = core.PreprocessDots(trimmed)
		}

		prog, cerr := expr.Compile(cleanExpr, expr.Env(env))
		if cerr != nil {
			fmt.Fprintf(w, "  %s %s  (syntax: %v)\n",
				paintAssert("✗", assertRed, color), src, cerr)
			failed++
			continue
		}
		out, rerr := expr.Run(prog, env)
		if rerr != nil {
			_, _ = fmt.Fprintf(w, "  %s %s  (runtime: %v)\n",
				paintAssert("✗", assertRed, color), src, rerr)
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
		_, _ = fmt.Fprintf(w, "  %s %s  (got %v)\n",
			paintAssert("✗", assertRed, color), src, out)
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
