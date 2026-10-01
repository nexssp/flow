package schema

import (
	"context"
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
		{"bad field line", []string{`@schema X {`, `  OnlyName`, `}`}, "expected `Name Type`"},
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
