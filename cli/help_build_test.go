package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestStandaloneServeCommandIsRemoved(t *testing.T) {
	if _, ok := commands["serve"]; ok {
		t.Fatal("standalone serve command is still registered")
	}
	var output bytes.Buffer
	PrintTopHelp(&output)
	if strings.Contains(output.String(), "  serve ") {
		t.Fatalf("top-level help still advertises the standalone serve command:\n%s", output.String())
	}
	if PrintCommandHelp(&bytes.Buffer{}, "serve") {
		t.Fatal("standalone serve help is still registered")
	}
}

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
