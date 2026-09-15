package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/expr-lang/expr"
)

func RunAssertions(result any, assertions []string, metrics *RunnerObserver) int {
	if len(assertions) == 0 {
		return 0
	}

	fmt.Printf("\n🧪 RUNNING TESTKIT ASSERTIONS (%d)...\n", len(assertions))

	raw, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ cannot marshal result for assertions: %v\n", err)

		return 1
	}

	env := make(map[string]any)
	_ = json.Unmarshal(raw, &env)

	if outputs, ok := env["outputs"].(map[string]any); ok {
		for k, v := range outputs {
			env[k] = v
			if stepMap, isMap := v.(map[string]any); isMap {
				for sk, sv := range stepMap {
					env[sk] = sv
				}
			}
		}
	}

	if metrics != nil {
		spent := metrics.TotalSpentMicros()
		tokens := metrics.TotalTokens()
		env["spent_micros"] = spent
		env["cost_usd"] = float64(spent) / 1_000_000.0
		env["total_tokens"] = tokens
	}

	failed := false

	for _, exprStr := range assertions {
		prog, err := expr.Compile(exprStr, expr.Env(env))
		if err != nil {
			fmt.Printf("  ❌ ERROR: invalid syntax -> %s (%v)\n", exprStr, err)

			failed = true

			continue
		}

		out, err := expr.Run(prog, env)
		if err != nil {
			fmt.Printf("  ❌ ERROR: runtime failure -> %s (%v)\n", exprStr, err)

			failed = true

			continue
		}

		if ok, isBool := out.(bool); isBool && ok {
			fmt.Printf("  ✅ PASS: %s\n", exprStr)

			continue
		}

		fmt.Printf("  ❌ FAIL: %s (evaluated to: %v)\n", exprStr, out)

		failed = true
	}

	fmt.Println(strings.Repeat("─", 80))

	if failed {
		fmt.Println("🛑 TESTKIT FAILED.")

		return 1
	}

	fmt.Println("🎉 ALL TESTKIT ASSERTIONS PASSED.")

	return 0
}
