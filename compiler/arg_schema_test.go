package compiler

import (
	"strings"
	"testing"
)

func TestResolveArgSchemaChecksPrimitiveLiteralTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		kind      ArgKind
		value     *Value
		wantError string
	}{
		{name: "string literal", kind: ArgString, value: &Value{Kind: ValueString, Str: "ready"}},
		{name: "bare string", kind: ArgString, value: &Value{Kind: ValueBare, Str: "ready"}},
		{name: "string rejects number", kind: ArgString, value: &Value{Kind: ValueNumber, Num: 3}, wantError: "expected string, got number"},
		{name: "string rejects missing value", kind: ArgString, value: nil, wantError: "expected string, got missing value"},
		{name: "integer literal", kind: ArgInt, value: &Value{Kind: ValueNumber, Num: 3}},
		{name: "integer rejects fraction", kind: ArgInt, value: &Value{Kind: ValueNumber, Num: 3.5}, wantError: "expected integer, got non-integer number"},
		{name: "integer rejects signed overflow", kind: ArgInt, value: &Value{Kind: ValueNumber, Num: float64(1 << 63)}, wantError: "expected integer, got non-integer number"},
		{name: "integer rejects string", kind: ArgInt, value: &Value{Kind: ValueString, Str: "3"}, wantError: "expected integer, got string"},
		{name: "boolean literal", kind: ArgBool, value: &Value{Kind: ValueBool, Bool: true}},
		{name: "boolean rejects string", kind: ArgBool, value: &Value{Kind: ValueString, Str: "true"}, wantError: "expected bool, got string"},
		{name: "dynamic reference deferred", kind: ArgInt, value: &Value{Kind: ValueRef, Ref: "amount"}},
		{name: "dynamic expression deferred", kind: ArgBool, value: &Value{Kind: ValueExpression, Expression: ".enabled"}},
		{name: "typed integer", kind: ArgInt, value: &Value{Kind: ValueTyped, Typed: int64(9)}},
		{name: "typed unsigned overflow", kind: ArgInt, value: &Value{Kind: ValueTyped, Typed: uint64(1 << 63)}, wantError: "expected integer, got typed value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := resolveArgSchema(nil, &Atom{
				Name: "test.action",
				Args: map[string]*Value{"value": tc.value},
			}, []ArgFieldSpec{{Name: "value", Kind: tc.kind}})
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("resolveArgSchema() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("resolveArgSchema() error = %v, want substring %q", err, tc.wantError)
			}
		})
	}
}

func TestBundleArgumentSchemasRekeysAliasesAndRejectsCollisions(t *testing.T) {
	t.Parallel()
	first := Bundle{
		ID:    "first",
		Alias: "local",
		ArgSchemas: map[string][]ArgFieldSpec{
			"schema.wrap": {{Name: "action", Kind: ArgCapabilityRef}},
		},
	}
	schemas, err := BundleArgumentSchemas(first)
	if err != nil {
		t.Fatalf("BundleArgumentSchemas() error = %v", err)
	}
	if _, ok := schemas["local.wrap"]; !ok {
		t.Fatalf("schemas = %#v, want aliased key local.wrap", schemas)
	}
	if _, ok := schemas["schema.wrap"]; ok {
		t.Fatalf("schemas retained unaliased key: %#v", schemas)
	}

	second := Bundle{ID: "second", ArgSchemas: map[string][]ArgFieldSpec{
		"local.wrap": {{Name: "other", Kind: ArgString}},
	}}
	if _, err := BundleArgumentSchemas(first, second); err == nil || !strings.Contains(err.Error(), "duplicate argument schema") {
		t.Fatalf("BundleArgumentSchemas() error = %v, want duplicate schema error", err)
	}
}
