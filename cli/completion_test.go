package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionRegistersNflowBinary(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "pwsh", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var script bytes.Buffer
			if err := Completion(shell, &script); err != nil {
				t.Fatalf("Completion(%q): %v", shell, err)
			}
			if !strings.Contains(script.String(), "nflow") {
				t.Fatalf("completion script for %q does not mention nflow", shell)
			}
			if strings.Contains(script.String(), "nexssflow") {
				t.Fatalf("completion script for %q refers to the wrong executable:\n%s", shell, script.String())
			}
		})
	}
}

func TestCompletionIncludesTestCommand(t *testing.T) {
	markers := map[string]string{
		"bash":       `subcommands="init run test`,
		"zsh":        `'test:run .nflow acceptance entrypoints in a directory'`,
		"pwsh":       `@('init', 'run', 'test'`,
		"powershell": `@('init', 'run', 'test'`,
	}
	for shell, marker := range markers {
		t.Run(shell, func(t *testing.T) {
			var script bytes.Buffer
			if err := Completion(shell, &script); err != nil {
				t.Fatalf("Completion(%q): %v", shell, err)
			}
			if !strings.Contains(script.String(), marker) {
				t.Fatalf("completion script for %q does not expose test: %q\n%s", shell, marker, script.String())
			}
		})
	}
}
