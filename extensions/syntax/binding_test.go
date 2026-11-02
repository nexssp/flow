package syntax

import (
	"context"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestNamedBindingPostfix_ParsesAfterTheBoundStep(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	parser := compiler.NewParserWithPrimaries(
		context.Background(),
		compiler.NewOperatorTable(bundle.Operators...),
		compiler.NewPrimaryExtensionTable(bundle.Primaries...),
		`start -> ai.cost.estimate as estimate -> ai.next`,
	)
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse named binding: %v", err)
	}
	pipe, ok := expr.(*compiler.PipeExpr)
	if !ok {
		t.Fatalf("parsed expression type = %T, want *compiler.PipeExpr", expr)
	}
	boundPipe, ok := pipe.L.(*compiler.PipeExpr)
	if !ok {
		t.Fatalf("left expression type = %T, want prior pipeline", pipe.L)
	}
	binding, ok := boundPipe.R.(*compiler.BindingExpr)
	if !ok {
		t.Fatalf("bound step type = %T, want *compiler.BindingExpr", boundPipe.R)
	}
	if binding.Name != "estimate" {
		t.Fatalf("binding name = %q, want estimate", binding.Name)
	}
	if atom, ok := binding.Inner.(*compiler.Atom); !ok || atom.Name != "ai.cost.estimate" {
		t.Fatalf("bound expression = %#v, want ai.cost.estimate", binding.Inner)
	}
}

func TestNamedBindingPostfix_RejectsInvalidAndReservedNames(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	for _, source := range []string{
		`ai.cost.estimate as`,
		`ai.cost.estimate as request.value`,
		`ai.cost.estimate as input`,
		`ai.cost.estimate as __root__`,
		`ai.cost.estimate as estimate as decision`,
	} {
		t.Run(source, func(t *testing.T) {
			parser := compiler.NewParserWithPrimaries(
				context.Background(),
				compiler.NewOperatorTable(bundle.Operators...),
				compiler.NewPrimaryExtensionTable(bundle.Primaries...),
				source,
			)
			if _, err := parser.Parse(); err == nil {
				t.Fatalf("parsing %q succeeded; want a syntax error", source)
			}
		})
	}
}
