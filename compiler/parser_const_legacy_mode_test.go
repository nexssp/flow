package compiler

import (
	"context"
	"testing"
)

func TestConstCallIsParsedAsOrdinaryAction(t *testing.T) {
	t.Parallel()

	expr, err := NewParser(context.Background(), NewOperatorTable(), `const(value: "Julia")`).Parse()
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("Parse() has type %T, want *Atom", expr)
	}
	if atom.Name != "const" || atom.DirectInput != nil || !atom.HasArgumentObject {
		t.Fatalf("Parse() = %+v, want ordinary const action with named arguments", atom)
	}
	if value := atom.Args["value"]; value == nil || value.Kind != ValueString || value.Str != "Julia" {
		t.Fatalf("const value argument = %+v, want string Julia", value)
	}
}

func TestConstScalarSpellingIsNotLoweredToDirectValue(t *testing.T) {
	t.Parallel()

	for _, source := range []string{`const "Julia"`, `const 42`, `const true`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := NewParser(context.Background(), NewOperatorTable(), source).Parse(); err == nil {
				t.Fatalf("Parse(%q) succeeded, want no runtime const lowering", source)
			}
		})
	}
}
