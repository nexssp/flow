package schema

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	symbolsext "github.com/nexssp/flow/extensions/symbols"
)

func runDirective(tb testing.TB, lines ...string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
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

func TestDirective_DefaultTags(t *testing.T) {
	t.Parallel()

	out, err := runDirective(t,
		`@schema Settings {`,
		"  Depth int `default:\"2\"`",
		`}`,
	)
	ktest.RequireNoError(t, err)
	s := SchemasFromMap(out)[0]
	values, err := ApplyDefaults(s, map[string]any{})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, values["depth"], 2)
}

func TestDirective_RejectsInvalidDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
	}{
		{name: "integer", line: `  Depth int ` + "`default:\"many\"`"},
		{name: "boolean", line: `  Enabled bool ` + "`default:\"perhaps\"`"},
		{name: "array JSON", line: `  Tags []string ` + "`default:\"not-json\"`"},
		{name: "array shape", line: `  Tags []string ` + "`default:\"{}\"`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := runDirective(t, `@schema Settings {`, tc.line, `}`)
			ktest.RequireErrorContains(t, err, `field "`)
			ktest.RequireErrorContains(t, err, "default")
		})
	}
}

func TestDirective_DecodesEscapedCompositeDefault(t *testing.T) {
	t.Parallel()

	out, err := runDirective(t,
		`@schema Settings {`,
		"  Options map[string]any `json:\"options\" default:\"{\\\"enabled\\\":true}\"`",
		`}`,
	)
	ktest.RequireNoError(t, err)
	values, err := ApplyDefaults(SchemasFromMap(out)[0], map[string]any{})
	ktest.RequireNoError(t, err)
	options, ok := values["options"].(map[string]any)
	ktest.RequireCondition(t, ok, "options default has type %T", values["options"])
	ktest.RequireEqual(t, options["enabled"], true)
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
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: []string{`@schema X {`, `  A string`, `}`},
		I:     0, Out: out, File: "<test>",
	})
	ktest.RequireNoError(t, err)

	_, err = handleDirective(context.Background(), compiler.DirectiveReq{
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
		res, err := handleDirective(context.Background(), compiler.DirectiveReq{
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

func TestCompiler_PreprocessResolvesSymbolsInTypedSchema(t *testing.T) {
	t.Parallel()
	source := strings.Join([]string{
		`@symbols memory { search, put, chat_turn }`,
		`@schema MemoryRequest {`,
		"  Operation string `json:\"operation\" default:\"memory.search\" validate:\"required,oneof=memory.search memory.put memory.chat_turn\"`",
		`}`,
	}, "\n")
	_, meta, err := compiler.Preprocess(
		t.Context(),
		compiler.NewDirectiveTable(symbolsext.Directive, Directive),
		source,
		"memory.nflow",
	)
	ktest.RequireNoError(t, err)

	schemas := SchemasFromMap(meta)
	ktest.RequireEqual(t, len(schemas), 1)
	field := schemas[0].Fields[0]
	ktest.RequireEqual(t, field.Tags["default"], "search")
	ktest.RequireEqual(t, field.Tags["validate"], "required,oneof=search put chat_turn")

	effective, err := ApplyDefaultsAndValidate(t.Context(), schemas[0], map[string]any{})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, effective["operation"], "search")
	_, err = ApplyDefaultsAndValidate(t.Context(), schemas[0], map[string]any{"operation": "delete"})
	ktest.RequireErrorContains(t, err, "must be one of")
}

func TestCompiler_PreprocessRejectsUnknownSymbolInTypedSchema(t *testing.T) {
	t.Parallel()
	source := strings.Join([]string{
		`@symbols memory { search put }`,
		`@schema MemoryRequest {`,
		"  Operation string `json:\"operation\" default:\"memory.missing\"`",
		`}`,
	}, "\n")
	_, _, err := compiler.Preprocess(
		t.Context(),
		compiler.NewDirectiveTable(symbolsext.Directive, Directive),
		source,
		"memory.nflow",
	)
	ktest.RequireErrorContains(t, err, `unknown symbol "memory.missing"`)
}

func TestCompiler_PreprocessResolvesSymbolBeforeTypedDefaultParsing(t *testing.T) {
	t.Parallel()
	source := strings.Join([]string{
		`@symbols http { created: "201" }`,
		`@schema Response {`,
		"  Status int `json:\"status\" default:\"http.created\" validate:\"eq=201\"`",
		`}`,
	}, "\n")
	_, meta, err := compiler.Preprocess(
		t.Context(),
		compiler.NewDirectiveTable(symbolsext.Directive, Directive),
		source,
		"response.nflow",
	)
	ktest.RequireNoError(t, err)

	schemas := SchemasFromMap(meta)
	ktest.RequireEqual(t, len(schemas), 1)
	ktest.RequireEqual(t, schemas[0].Fields[0].Tags["default"], "201")
	effective, err := ApplyDefaultsAndValidate(t.Context(), schemas[0], map[string]any{})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, effective["status"], 201)
}
