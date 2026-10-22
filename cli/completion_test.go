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
