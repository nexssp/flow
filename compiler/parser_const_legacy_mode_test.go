package compiler

import (
	"context"
	"testing"
)

func TestConstArgumentSyntaxMarksOnlyAtBraceAsLegacy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		source     string
		wantLegacy bool
	}{
		{name: "parenthesized", source: `const(value: "42")`},
		{name: "legacy block", source: `const @{ value: "42" }`, wantLegacy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr, err := NewParser(context.Background(), NewOperatorTable(), tc.source).Parse()
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			atom, ok := expr.(*Atom)
			if !ok {
				t.Fatalf("expression = %T, want *Atom", expr)
			}
			if atom.LegacyArgumentBlock != tc.wantLegacy {
				t.Fatalf("LegacyArgumentBlock = %t, want %t", atom.LegacyArgumentBlock, tc.wantLegacy)
			}
		})
	}
}
