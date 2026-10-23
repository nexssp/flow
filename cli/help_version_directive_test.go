package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestLintHelpNamesNFlowVersionDirective(t *testing.T) {
	var output bytes.Buffer
	if !PrintCommandHelp(&output, "lint") {
		t.Fatal("lint help is not registered")
	}
	text := output.String()
	if !strings.Contains(text, "@nflow_version") {
		t.Fatalf("lint help does not name the compiler-version guard:\n%s", text)
	}
	if strings.Contains(text, "@flow_version") {
		t.Fatalf("lint help advertises the removed directive:\n%s", text)
	}
}
