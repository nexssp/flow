package schema

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(tb testing.TB, lines ...string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, err
}

func TestDirective_BasicDeclaration(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@schema User {`,
		`  Name string`,
		`  Age  int`,
		`}`,
	)
	ktest.RequireNoError(t, err)

	schemas := SchemasFromMap(out)
	ktest.RequireEqual(t, len(schemas), 1)
	ktest.RequireEqual(t, schemas[0].Name, "User")
	ktest.RequireEqual(t, len(schemas[0].Fields), 2)
	ktest.RequireEqual(t, schemas[0].Fields[0].Name, "Name")
	ktest.RequireEqual(t, schemas[0].Fields[0].Kind, KindString)
	ktest.RequireEqual(t, schemas[0].Fields[1].Kind, KindInt)
}

func TestDirective_WithStructKeyword(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@schema Plan struct {`,
		`  Summary string`,
		`}`,
	)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, SchemasFromMap(out)[0].Name, "Plan")
}

func TestDirective_WithTags(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@schema User {`,
		"  Name  string `json:\"name\"  validate:\"required\"`",
		"  Email string `json:\"email\" validate:\"email\"`",
		`}`,
	)
	ktest.RequireNoError(t, err)

	fields := SchemasFromMap(out)[0].Fields
	ktest.RequireEqual(t, fields[0].JSONName, "name")
	ktest.RequireEqual(t, fields[0].Tags["validate"], "required")
	ktest.RequireEqual(t, fields[1].JSONName, "email")
}

func TestDirective_SliceAndMapAndPointer(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@schema X {`,
		`  Tags    []string`,
		`  Attrs   map[string]any`,
		`  Parent  *User`,
		`}`,
	)
	ktest.RequireNoError(t, err)

	fields := SchemasFromMap(out)[0].Fields
	ktest.RequireCondition(t, fields[0].Slice, "Tags should be slice")
	ktest.RequireCondition(t, fields[1].Map, "Attrs should be map")
	ktest.RequireCondition(t, fields[2].Pointer, "Parent should be pointer")
}

func TestDirective_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		lines   []string
		wantSub string
	}{
		{"missing brace", []string{`@schema X`}, "expected"},
		{"empty name", []string{`@schema {`, `  A string`, `}`}, "name is required"},
		{"empty body", []string{`@schema X {`, `}`}, "at least one field"},
		{"unknown embed", []string{`@schema X {`, `  OnlyName`, `}`}, "not declared before this point"},
		{"too many tokens", []string{`@schema X {`, `  Name string extra`, `}`}, "expected `Name Type` or `Embed`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := runDirective(t, c.lines...)
			ktest.RequireErrorContains(t, err, c.wantSub)
		})
	}
}

func TestDirective_DuplicateSchema(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{`@schema X {`, `  A string`, `}`},
		I:     0, Out: out, File: "<test>",
	})
	ktest.RequireNoError(t, err)

	_, err = handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{`@schema X {`, `  B string`, `}`},
		I:     0, Out: out, File: "<test>",
	})
	ktest.RequireErrorContains(t, err, "duplicate declaration")
}

func TestDirective_DuplicateField(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t,
		`@schema X {`,
		`  A string`,
		`  A string`,
		`}`,
	)
	ktest.RequireErrorContains(t, err, "duplicate field")
}

// runSchemaSequence walks every @schema block in order, sharing the
// same meta map, so composition across declarations can be exercised
// end to end.
func runSchemaSequence(tb testing.TB, lines ...string) ([]Schema, error) {
	tb.Helper()
	out := map[string]any{}
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "@schema") {
			i++
			continue
		}
		res, err := handleDirective(context.Background(), core.DirectiveReq{
			Lines: lines,
			I:     i,
			Out:   out,
			File:  "<test>",
		})
		if err != nil {
			return SchemasFromMap(out), err
		}
		i = res.Next
	}
	return SchemasFromMap(out), nil
}

func TestDirective_Composition(t *testing.T) {
	t.Parallel()

	t.Run("embed flattens fields in declaration order", func(t *testing.T) {
		t.Parallel()
		schemas, err := runSchemaSequence(t,
			`@schema Common {`,
			"  JSON  bool `json:\"json\"`",
			"  Quiet bool `json:\"quiet\"`",
			`}`,
			`@schema Pack {`,
			`  Common`,
			"  Target string `json:\"target\"`",
			`}`,
		)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(schemas), 2)

		names := make([]string, len(schemas[1].Fields))
		for i, f := range schemas[1].Fields {
			names[i] = f.Name
		}
		ktest.RequireEqual(t, names, []string{"JSON", "Quiet", "Target"})
	})

	t.Run("embed of undeclared schema", func(t *testing.T) {
		t.Parallel()
		_, err := runSchemaSequence(t,
			`@schema Pack {`,
			`  Missing`,
			`}`,
		)
		ktest.RequireErrorContains(t, err, "not declared before this point")
	})

	t.Run("self-embed", func(t *testing.T) {
		t.Parallel()
		_, err := runSchemaSequence(t,
			`@schema A {`,
			`  A`,
			`}`,
		)
		ktest.RequireErrorContains(t, err, "cannot embed itself")
	})

	t.Run("collision with embedded field", func(t *testing.T) {
		t.Parallel()
		_, err := runSchemaSequence(t,
			`@schema Common {`,
			"  JSON bool `json:\"json\"`",
			`}`,
			`@schema Pack {`,
			`  Common`,
			"  JSON bool `json:\"json2\"`",
			`}`,
		)
		ktest.RequireErrorContains(t, err, `duplicate field "JSON"`)
	})

	t.Run("tags on embed line rejected", func(t *testing.T) {
		t.Parallel()
		_, err := runSchemaSequence(t,
			`@schema Common {`,
			"  JSON bool `json:\"json\"`",
			`}`,
			`@schema Pack {`,
			"  Common `json:\"c\"`",
			`}`,
		)
		ktest.RequireErrorContains(t, err, "must not carry tags")
	})

	t.Run("multiple embeds flatten in order", func(t *testing.T) {
		t.Parallel()
		schemas, err := runSchemaSequence(t,
			`@schema A {`,
			"  Foo string `json:\"foo\"`",
			`}`,
			`@schema B {`,
			"  Bar int `json:\"bar\"`",
			`}`,
			`@schema C {`,
			`  A`,
			`  B`,
			`}`,
		)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(schemas), 3)

		names := make([]string, len(schemas[2].Fields))
		for i, f := range schemas[2].Fields {
			names[i] = f.Name
		}
		ktest.RequireEqual(t, names, []string{"Foo", "Bar"})
	})

	t.Run("resolved fields never carry Embed", func(t *testing.T) {
		t.Parallel()
		schemas, err := runSchemaSequence(t,
			`@schema Common {`,
			"  JSON bool `json:\"json\"`",
			`}`,
			`@schema Pack {`,
			`  Common`,
			`}`,
		)
		ktest.RequireNoError(t, err)
		for _, f := range schemas[1].Fields {
			ktest.RequireEqual(t, f.Embed, "")
		}
	})
}
