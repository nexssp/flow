package runner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
)

// RunAssertions evaluates every assertion against the flow result.
//
// The environment exposed to an assertion is built in this order:
//
//  1. Every named node output, keyed by its final path segment
//     (tasks.greet.output becomes env["greet"] and every field inside
//     it is flattened one level up so { status: "ok" } becomes
//     env["status"]).
//  2. The final result of the flow, whose keys overwrite anything
//     already present. This makes `n == 64` work whether `n` came from
//     the last node or was threaded through the pipeline.
//
// At verbosity 0, silence is success. At verbosity >= 1, every
// assertion prints one line.
func RunAssertions(result any, assertions []string, metrics *RunnerObserver, verbosity int) int {
	if len(assertions) == 0 {
		return 0
	}

	env := make(map[string]any)

	raw, err := json.Marshal(result)
	if err != nil {
		fmt.Printf("✗ cannot encode result: %v\n", err)
		return 1
	}
	_ = json.Unmarshal(raw, &env)

	// Layer 1: named node outputs.
	if outputs, ok := env["outputs"].(map[string]any); ok {
		for path, v := range outputs {
			short := lastSegment(path)
			env[short] = v

			// Flatten one level for convenience: { status: "ok" }
			// becomes env["status"] = "ok".
			if m, isMap := v.(map[string]any); isMap {
				for k, val := range m {
					if _, exists := env[k]; !exists {
						env[k] = val
					}
				}
			}
		}
	}

	// Layer 2: the flow result wins.
	if r, ok := env["result"].(map[string]any); ok {
		for k, v := range r {
			env[k] = v
		}
	}

	if metrics != nil {
		env["spent_micros"] = metrics.TotalSpentMicros()
		env["cost_usd"] = float64(metrics.TotalSpentMicros()) / 1_000_000.0
		env["total_tokens"] = metrics.TotalTokens()
	}

	if verbosity >= 1 {
		fmt.Printf("\n🧪 ASSERTIONS (%d)\n", len(assertions))
	}

	var failed int

	for _, src := range assertions {
		prog, cerr := expr.Compile(src, expr.Env(env))
		if cerr != nil {
			fmt.Printf("  ✗ %s  (syntax: %v)\n", src, cerr)
			failed++
			continue
		}

		out, rerr := expr.Run(prog, env)
		if rerr != nil {
			fmt.Printf("  ✗ %s  (runtime: %v)\n", src, rerr)
			failed++
			continue
		}

		if b, isBool := out.(bool); isBool && b {
			if verbosity >= 1 {
				fmt.Printf("  ✓ %s\n", src)
			}
			continue
		}

		fmt.Printf("  ✗ %s  (got %v)\n", src, out)
		failed++
	}

	if failed > 0 {
		if verbosity >= 1 {
			fmt.Println(strings.Repeat("─", 80))
		}
		return 1
	}

	if verbosity >= 1 {
		fmt.Println(strings.Repeat("─", 80))
		fmt.Println("✓ all assertions passed")
	}

	return 0
}

// lastSegment returns the last dot-separated segment of a path.
// "tasks.math.square.output" -> "output" (called only on outputs so
// callers use the field name, not the node name). If you would rather
// key on the node name, change this to return the segment before the
// final dot.
func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}
