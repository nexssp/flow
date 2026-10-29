package compiler

import (
	"context"
	"testing"
)

func TestParseExampleModifierJSONPayload(t *testing.T) {
	source := `noop:example='{"account_id":"acct-demo"}'`
	ast, err := NewParserWithPrimaries(
		context.Background(),
		NewOperatorTable(),
		NewPrimaryExtensionTable(),
		source,
	).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	atom, ok := ast.(*Atom)
	if !ok {
		t.Fatalf("Parse() returned %T, want *Atom", ast)
	}
	if len(atom.Modifiers) != 1 || atom.Modifiers[0] != `example={"account_id":"acct-demo"}` {
		t.Fatalf("modifiers = %#v, want one JSON example modifier", atom.Modifiers)
	}
}
