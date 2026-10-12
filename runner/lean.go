package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	compiler "github.com/nexssp/flow/compiler"
)

// RunLean executes an embedded flow source using only the provided bundles.
// It contains zero CLI, testkit, or development dependencies.
func RunLean(ctx context.Context, source, name string, bundles []compiler.Bundle, args []string) int {
	cfg, err := BuildConfig(bundles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		return 2
	}

	payload := map[string]any{}
	for _, arg := range args {
		if arg != "" && arg[0] == '{' {
			if payloadErr := json.Unmarshal([]byte(arg), &payload); payloadErr != nil {
				fmt.Fprintf(os.Stderr, "payload error: %v\n", payloadErr)
				return 2
			}
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
			data, err := json.Marshal(ex.Output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "output encoding error: %v\n", err)
				return 1
			}
			fmt.Println(string(data))
		}
	}
	return 0
}
