package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelfBenchHelp(t *testing.T) {
	var output bytes.Buffer
	if !PrintCommandHelp(&output, "self") {
		t.Fatal("PrintCommandHelp(self) returned false")
	}
	for _, want := range []string{
		"nflow self bench [filter...] [--path FILE_OR_DIR] [--iterations N] [--warmup N] [--json] [--list]",
		"--iterations N        measured iterations (default 50; range 1..1000000)",
		"--warmup N            unreported warmups (default 3; range 0..1000000)",
		"--list                list matching scenarios without running them",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("self help does not contain %q:\n%s", want, output.String())
		}
	}
}

func TestSelfBenchCompletionFlagsDoNotCompleteScenarioValues(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "pwsh"} {
		t.Run(shell, func(t *testing.T) {
			var script bytes.Buffer
			if err := Completion(shell, &script); err != nil {
				t.Fatalf("Completion(%q): %v", shell, err)
			}
			text := script.String()
			for _, flag := range []string{"--path", "--iterations", "--warmup", "--json", "--list"} {
				if !strings.Contains(text, flag) {
					t.Errorf("%s completion does not contain %s", shell, flag)
				}
			}
			switch shell {
			case "bash":
				if !strings.Contains(text, `[[ "$sub" == "self" && "$op" == "bench" && ( "$prev" == "--path"`) {
					t.Error("bash completion does not suppress suggestions after value-taking self bench flags")
				}
			case "zsh":
				if !strings.Contains(text, "elif [[ \"${words[3]}\" == \"bench\" ]]; then") {
					t.Error("zsh self bench completion branch is missing")
				}
			case "pwsh":
				if !strings.Contains(text, "'self' { if ($tokens.Count -ge 3 -and $tokens[2].ToString() -eq 'bench')") {
					t.Error("PowerShell self bench completion branch is missing")
				}
			}
		})
	}
}
