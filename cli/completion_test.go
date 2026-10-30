package cli

import (
	"bytes"
	"slices"
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
	for _, shell := range SupportedShells {
		t.Run(shell, func(t *testing.T) {
			var script bytes.Buffer
			if err := Completion(shell, &script); err != nil {
				t.Fatalf("Completion(%q): %v", shell, err)
			}
			if !completionIncludesCommand(shell, script.String(), "test") {
				t.Fatalf("completion script for %q does not expose the test command", shell)
			}
		})
	}
}

func completionIncludesCommand(shell, script, command string) bool {
	for line := range strings.SplitSeq(script, "\n") {
		line = strings.TrimSpace(line)
		switch shell {
		case "bash":
			commands, found := strings.CutPrefix(line, "subcommands=")
			if found {
				if slices.Contains(strings.Fields(strings.Trim(commands, `"`)), command) {
					return true
				}
			}
		case "zsh":
			entry := strings.Trim(line, "'")
			name, _, found := strings.Cut(entry, ":")
			if found && name == command {
				return true
			}
		case "pwsh", "powershell":
			if !strings.Contains(line, "$subcommands") {
				continue
			}
			values, found := strings.CutPrefix(line, "$subcommands = @(")
			if !found {
				continue
			}
			values = strings.TrimSuffix(values, ")")
			for candidate := range strings.SplitSeq(values, ",") {
				if strings.Trim(strings.TrimSpace(candidate), "'") == command {
					return true
				}
			}
		}
	}
	return false
}
