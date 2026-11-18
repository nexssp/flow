package compiler

import (
	"context"
	"strings"
	"testing"
)

func TestLegacyConstArgumentBlocksAreRejectedWithReplacementHint(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`const @{ value: "Julia" }`,
		`runtime.const @{ status: "ready" }`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := NewParser(context.Background(), NewOperatorTable(), source).Parse()
			const want = "legacy `const @{...}` arguments are not supported; use `const <value>` for a scalar or `const(key: value)` to construct an object"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Parse() error = %v, want diagnostic containing %q", err, want)
			}
		})
	}
}

func TestCurrentConstArgumentFormsRemainSupported(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`const "Julia"`,
		`const sla.p1`,
		`const .response.id`,
		`const(value: "Julia")`,
		`const(status: "ready")`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := NewParser(context.Background(), NewOperatorTable(), source).Parse(); err != nil {
				t.Fatalf("Parse(%q): %v", source, err)
			}
		})
	}
}

func TestCurrentConstDirectCollectionsRemainSupported(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		source string
		kind   ValueKind
	}{
		{source: `const { status: "ready" }`, kind: ValueMap},
		{source: `const ["Julia", 42]`, kind: ValueSlice},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			expr, err := NewParser(context.Background(), NewOperatorTable(), tc.source).Parse()
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.source, err)
			}
			atom, ok := expr.(*Atom)
			if !ok || atom.DirectInput == nil || atom.DirectInput.Kind != tc.kind || atom.HasArgumentObject {
				t.Fatalf("Parse(%q) = %#v, want direct input kind %v", tc.source, expr, tc.kind)
			}
		})
	}
}
