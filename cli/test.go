package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const testModeFlag = "--nflow-test-mode"

func runTestInInvocation(ctx context.Context, inv *invocation, args []string) int {
	if len(args) > 1 {
		return fatalf("usage: nflow test [directory]")
	}

	root := "./spec"
	if len(args) == 1 {
		root = args[0]
	}

	files, err := discoverTestFlows(root)
	if err != nil {
		return fatalf("test: %v", err)
	}
	if len(files) == 0 {
		return fatalf("test: no .nflow entrypoints found in %q", root)
	}

	return runTestFiles(files, func(path string) int {
		return runFlowInInvocation(ctx, inv, []string{path, testModeFlag})
	})
}

// discoverTestFlows matches the repository's integrated-spec layout:
// <directory>/<scenario>/*.nflow. Nested helpers are loaded by their parent
// flow and are not independent test entrypoints.
func discoverTestFlows(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", root, err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		scenarioDir := filepath.Join(root, entry.Name())
		scenarioEntries, err := os.ReadDir(scenarioDir)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", scenarioDir, err)
		}
		for _, scenarioEntry := range scenarioEntries {
			if scenarioEntry.IsDir() || !strings.HasSuffix(scenarioEntry.Name(), ".nflow") {
				continue
			}
			files = append(files, filepath.Join(scenarioDir, scenarioEntry.Name()))
		}
	}

	sort.Strings(files)
	return files, nil
}

func runTestFiles(files []string, run func(string) int) int {
	status := 0
	for _, path := range files {
		fmt.Fprintf(os.Stdout, "=== %s ===\n", path)
		if run(path) == 0 {
			fmt.Fprintf(os.Stdout, "PASS: %s\n", path)
			continue
		}
		fmt.Fprintf(os.Stderr, "FAIL: %s\n", path)
		status = 1
	}
	return status
}
