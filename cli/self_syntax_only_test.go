package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelfHelpDocumentsSyntaxOnlyFlag(t *testing.T) {
	var output bytes.Buffer
	if !PrintCommandHelp(&output, "self") {
		t.Fatal("PrintCommandHelp(self) returned false")
	}
	for _, want := range []string{
		"--syntax-only         compile selected fixtures without executing flows or probing language runtimes",
		"nflow self test --syntax-only",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("self help does not contain %q:\n%s", want, output.String())
		}
	}
}
