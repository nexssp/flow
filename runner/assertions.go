package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"

	"github.com/expr-lang/expr"
)

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

// RunAssertions ewaluuje @assert: z meta plus CLI --assert.
//
// Env dostępny dla asercji:
//
//	result       — output ostatniej akcji w pipeline
//	duration_ms  — całkowity czas wykonania
//	actions      — lista nazw wykonanych akcji
//
// Verbosity < 1 → cisza przy sukcesie.
func RunAssertions(w io.Writer, result any, durationMS int64, actions, asserts []string, verbosity int) int {
	if len(asserts) == 0 {
		return 0
	}

	normalized := JSONMapView(result)
	env := map[string]any{
		"result":      normalized,
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
		prog, cerr := expr.Compile(src, expr.Env(env))
		if cerr != nil {
			_, _ = fmt.Fprintf(w, "  ✗ %s  (syntax: %v)\n", src, cerr)
			failed++
			continue
		}
		out, rerr := expr.Run(prog, env)
		if rerr != nil {
			_, _ = fmt.Fprintf(w, "  ✗ %s  (runtime: %v)\n", src, rerr)
			failed++
			continue
		}
		if b, ok := out.(bool); ok && b {
			if verbosity >= 1 {
				_, _ = fmt.Fprintf(w, "  ✓ %s\n", src)
			}
			continue
		}
		_, _ = fmt.Fprintf(w, "  ✗ %s  (got %v)\n", src, out)
		failed++
	}

	if failed > 0 {
		return 1
	}
	if verbosity >= 1 {
		_, _ = fmt.Fprintln(w, "✓ all assertions passed")
	}
	return 0
}
