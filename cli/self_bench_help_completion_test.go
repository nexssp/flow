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
		"nflow self bench [filter...] [--path FILE_OR_DIR] [--iterations N] [--warmup N] [--profile] [--cpu-profile FILE] [--json] [--list]",
		"--iterations N        measured iterations (default 50; range 1..1000000)",
		"--warmup N            unreported warmups (default 3; range 0..1000000)",
		"--profile             include action call tree with inclusive/self wall time (adds hook overhead)",
		"--cpu-profile FILE    save Go CPU profile and show top functions (one scenario only; no action hook)",
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
			for _, flag := range []string{"--path", "--iterations", "--warmup", "--profile", "--cpu-profile", "--json", "--list", "--syntax-only"} {
				if !strings.Contains(text, flag) {
					t.Errorf("%s completion does not contain %s", shell, flag)
				}
			}
			switch shell {
			case "bash":
				if !strings.Contains(text, `[[ "$sub" == "self" && "$op" == "bench" && ( "$prev" == "--path"`) || !strings.Contains(text, `"$prev" == "--cpu-profile" )`) {
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
				if !strings.Contains(text, "$tokens[-2].ToString() -in @('--path', '--iterations', '--warmup', '--cpu-profile')") {
					t.Error("PowerShell completion does not consume the CPU profile path value")
				}
			}
		})
	}
}
