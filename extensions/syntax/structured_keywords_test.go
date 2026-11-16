package syntax

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	assertExtension "github.com/nexssp/flow/extensions/assert"
)

func TestStructuredKeywords_RejectInvalidBoundaryCharacter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{"parallel", `parallel { profile: runtime.noop § }`},
		{"race", `race { runtime.noop § }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := newStructuredKeywordParser(tc.source, false).Parse()
			if err == nil || !strings.Contains(err.Error(), `unexpected character "§"`) {
				t.Fatalf("parse error = %v, want unexpected character diagnostic", err)
			}
		})
	}
}

func TestStructuredKeywords_PreserveNestedExpressionPunctuation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{"parallel", `parallel { check: assert(.score > 0), next: runtime.noop }`},
		{"race", `race { assert(contains(["a","b"], .score)), runtime.noop }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newStructuredKeywordParser(tc.source, true).Parse(); err != nil {
				t.Fatalf("parse valid nested expression: %v", err)
			}
		})
	}
}

func newStructuredKeywordParser(source string, includeAssert bool) *compiler.Parser {
	bundle := Bundle(nil)
	primaries := append([]compiler.PrimaryExtension(nil), bundle.Primaries...)
	if includeAssert {
		primaries = append(primaries, assertExtension.Bundle(nil).Primaries...)
	}
	return compiler.NewParserWithPrimaries(
		context.Background(),
		compiler.NewOperatorTable(bundle.Operators...),
		compiler.NewPrimaryExtensionTable(primaries...),
		source,
	)
}
