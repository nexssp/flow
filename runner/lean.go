package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nexssp/flow/core"
)

// RunLean executes an embedded flow source using only the provided bundles.
// It contains zero CLI, testkit, or development dependencies.
func RunLean(ctx context.Context, source, name string, bundles []core.Bundle, args []string) int {
	cfg, err := BuildConfig(bundles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		return 2
	}

	payload := map[string]any{}
	for _, arg := range args {
		if len(arg) > 0 && arg[0] == '{' {
			_ = json.Unmarshal([]byte(arg), &payload)
		}
	}

	ex, err := Execute(ctx, cfg, source, name, payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "execution error: %v\n", err)
		return 1
	}

	if ex.Output != nil {
		if str, ok := ex.Output.(string); ok {
			fmt.Println(str)
		} else {
			data, _ := json.Marshal(ex.Output)
			fmt.Println(string(data))
		}
	}
	return 0
}
