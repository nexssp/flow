package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildHelpMatchesSupportedOptions(t *testing.T) {
	var output bytes.Buffer
	if !PrintCommandHelp(&output, "build") {
		t.Fatal("build help is not registered")
	}
	text := output.String()
	for _, want := range []string{"[-o <output>]", "--debug", "--no-trimpath", "--all-bundles", "default: source filename"} {
		if !strings.Contains(text, want) {
			t.Errorf("build help is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--target") || strings.Contains(text, "output binary (required)") {
		t.Fatalf("build help advertises an unsupported or incorrect contract:\n%s", text)
	}
}
