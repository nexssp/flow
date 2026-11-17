package compiler

import "testing"

func TestAnalyzeTypedAtomValues(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}

	t.Run("nil expression checker", func(t *testing.T) {
		atom := &Atom{
			Name:        "test.unregistered",
			DirectInput: &Value{Kind: ValueExpression, Expression: ".name"},
		}
		if _, err := AnalyzeTypedExpression(TypeAnalysisContext{Resolver: resolver}, atom); err != nil {
			t.Fatalf("analyze atom without expression checker: %v", err)
		}
	})

	t.Run("direct input on unregistered atom", func(t *testing.T) {
		calls := 0
		ctx := TypeAnalysisContext{
			Resolver: resolver,
			CheckExpression: func(source string, _ TypeAnalysisContext, _ Position) (TypeDescriptor, error) {
				calls++
				if source != ".name" {
					t.Errorf("checked expression = %q, want %q", source, ".name")
				}
				return TypeDescriptor{}, nil
			},
		}
		atom := &Atom{
			Name:        "test.unregistered",
			DirectInput: &Value{Kind: ValueExpression, Expression: ".name"},
		}
		if _, err := AnalyzeTypedExpression(ctx, atom); err != nil {
			t.Fatalf("analyze direct-input expression: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expression checker calls = %d, want 1", calls)
		}
	})
}
