package compiler

import (
	"context"
	"testing"
)

func TestParseParenthesizedNamedArguments(t *testing.T) {
	t.Parallel()

	expr, err := NewParser(context.Background(), NewOperatorTable(),
		`wrap(key: "greeting", values: [1, "2"], nested: { enabled: true })`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expression = %T, want *Atom", expr)
	}
	if !atom.HasArgumentObject || len(atom.Args) != 3 || len(atom.Params) != 0 {
		t.Fatalf("atom = %+v, want three typed named arguments", atom)
	}
	if value := atom.Args["key"]; value == nil || value.Kind != ValueString || value.Str != "greeting" {
		t.Fatalf("key = %+v, want string greeting", value)
	}
	values := atom.Args["values"]
	if values == nil || values.Kind != ValueSlice || len(values.Slice) != 2 ||
		values.Slice[0].Kind != ValueNumber || values.Slice[1].Kind != ValueString {
		t.Fatalf("values = %+v, want a typed number and string", values)
	}
	if nested := atom.Args["nested"]; nested == nil || nested.Kind != ValueMap ||
		len(nested.Map) != 1 || nested.Map[0].Value.Kind != ValueBool {
		t.Fatalf("nested = %+v, want an object with a boolean", nested)
	}
}

func TestParseParenthesizedArgumentsAfterModifiers(t *testing.T) {
	t.Parallel()

	const source = `request.action :timeout=3s (id: .id)`
	expr, err := NewParser(context.Background(), NewOperatorTable(), source).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expression = %T, want *Atom", expr)
	}
	if atom.Name != "request.action" || len(atom.Modifiers) != 1 || atom.Modifiers[0] != "timeout=3s" {
		t.Fatalf("atom = %+v, want request.action with timeout=3s", atom)
	}
	if value := atom.Args["id"]; value == nil || value.Kind != ValueRef || value.Ref != "id" {
		t.Fatalf("id = %+v, want reference to the current input's id field", value)
	}
}

func TestParseParenthesizedArgumentsIgnoreBlockCommentDelimiters(t *testing.T) {
	t.Parallel()

	const source = `request.action(value: (input.count/* ) */ + 2))`
	expr, err := NewParser(context.Background(), NewOperatorTable(), source).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expression = %T, want *Atom", expr)
	}
	value := atom.Args["value"]
	if value == nil || value.Kind != ValueExpression || value.Expression != `input.count/* ) */ + 2` {
		t.Fatalf("value = %+v, want expression with its block comment preserved", value)
	}
}

func TestParseArrayLiteralIgnoresBlockCommentDelimiters(t *testing.T) {
	t.Parallel()

	const source = `[(input.count/* ] */ + 2)]`
	expr, err := NewParser(context.Background(), NewOperatorTable(), source).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	valueExpr, ok := expr.(*ValueExpr)
	if !ok || valueExpr.Value == nil || valueExpr.Value.Kind != ValueSlice || len(valueExpr.Value.Slice) != 1 {
		t.Fatalf("expression = %#v, want a direct one-item array", expr)
	}
	value := valueExpr.Value.Slice[0]
	if value.Kind != ValueExpression || value.Expression != `input.count/* ] */ + 2` {
		t.Fatalf("array item = %+v, want expression with its block comment preserved", value)
	}
}

func TestParseTypedPipelineStarts(t *testing.T) {
	t.Parallel()

	t.Run("quoted numeric text remains string", func(t *testing.T) {
		expr, err := NewParser(context.Background(), NewOperatorTable(), `"42"`).Parse()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		literal, ok := expr.(*LiteralExpr)
		if !ok || literal.Val != "42" {
			t.Fatalf("expression = %#v, want string literal", expr)
		}
	})

	t.Run("array keeps element types", func(t *testing.T) {
		expr, err := NewParser(context.Background(), NewOperatorTable(), `[1, "2", true]`).Parse()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		valueExpr, ok := expr.(*ValueExpr)
		if !ok || valueExpr.Value == nil || valueExpr.Value.Kind != ValueSlice {
			t.Fatalf("expression = %#v, want a direct array value", expr)
		}
		values := valueExpr.Value.Slice
		if len(values) != 3 || values[0].Kind != ValueNumber || values[1].Kind != ValueString || values[2].Kind != ValueBool {
			t.Fatalf("array values = %#v, want number, string, boolean", values)
		}
	})

	t.Run("config reference can start a pipeline", func(t *testing.T) {
		expr, err := NewParser(context.Background(), NewOperatorTable(), `@config.retries`).Parse()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		valueExpr, ok := expr.(*ValueExpr)
		if !ok || valueExpr.Value == nil || valueExpr.Value.Kind != ValueRef || valueExpr.Value.Ref != "config.retries" {
			t.Fatalf("expression = %#v, want a direct config reference", expr)
		}
	})

	t.Run("flag reference can start a pipeline", func(t *testing.T) {
		expr, err := NewParser(context.Background(), NewOperatorTable(), `@flag.name`).Parse()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		valueExpr, ok := expr.(*ValueExpr)
		if !ok || valueExpr.Value == nil || valueExpr.Value.Kind != ValueRef || valueExpr.Value.Ref != "flag.name" {
			t.Fatalf("expression = %#v, want a direct flag reference", expr)
		}
	})

	t.Run("input reference can start a pipeline", func(t *testing.T) {
		expr, err := NewParser(context.Background(), NewOperatorTable(), `.user.name`).Parse()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		valueExpr, ok := expr.(*ValueExpr)
		if !ok || valueExpr.Value == nil || valueExpr.Value.Kind != ValueRef || valueExpr.Value.Ref != "user.name" {
			t.Fatalf("expression = %#v, want a direct input reference", expr)
		}
	})
}

func TestParseParenthesizedLegacyKeyValueStillWorks(t *testing.T) {
	t.Parallel()

	expr, err := NewParser(context.Background(), NewOperatorTable(), `runtime.const(value=hi)`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok || atom.Params["value"] != "hi" {
		t.Fatalf("expression = %#v, want legacy value parameter", expr)
	}
}
