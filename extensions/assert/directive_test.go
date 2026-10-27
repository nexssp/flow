package assert

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
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

func TestDirective_ColonForm(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert: result == "ok"`)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, len(asserts), 1)
	ktest.RequireEqual(t, asserts[0], `result == "ok"`)
}

func TestDirective_SpaceForm(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert result == "ok"`)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, len(asserts), 1)
	ktest.RequireEqual(t, asserts[0], `result == "ok"`)
}

func TestDirective_Accumulates(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	for _, line := range []string{`@assert: a == 1`, `@assert: b == 2`} {
		_, err := handleDirective(context.Background(), compiler.DirectiveReq{
			Lines: []string{line},
			I:     0,
			Out:   out,
			File:  "<test>",
		})
		ktest.RequireNoError(t, err)
	}

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts, []string{"a == 1", "b == 2"})
}

func TestDirective_EmptyRejected(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@assert:`)
	ktest.RequireErrorContains(t, err, "expression is required")
}

func TestDirective_WhitespaceTrimmed(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert:    result.x == 1   `)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts[0], "result.x == 1")
}

func TestDirective_StripsInlineComment(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert: x > 0  # positive`)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireLen(t, asserts, 1)
	ktest.RequireEqual(t, asserts[0], "x > 0")
}

func TestDirective_PreservesIteratorInsideAll(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert: all(items, # != "x")  # check`)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts[0], `all(items, # != "x")`)
}

func TestDirective_BlockForm(t *testing.T) {
	t.Parallel()
	lines := []string{
		`@assert {`,
		`  result.message == "Guest"`,
		`  result.state.depth == 1`,
		`  result.optional == nil`,
		`}`,
	}
	out, err := runDirective(t, lines...)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts, []string{
		`result.message == "Guest"`,
		`result.state.depth == 1`,
		`result.optional == nil`,
	})
	positions, _ := out[assertionPositionsKey].([]compiler.Position)
	ktest.RequireEqual(t, positions, []compiler.Position{
		{File: "<test>", Line: 2},
		{File: "<test>", Line: 3},
		{File: "<test>", Line: 4},
	})
}

func TestDirective_BlockFormSkipsCommentsAndBlanks(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@assert {`,
		`  # comment`,
		``,
		`  result.ok == true,`,
		`}`,
	)
	ktest.RequireNoError(t, err)
	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts, []string{`result.ok == true`})
	positions, _ := out[assertionPositionsKey].([]compiler.Position)
	ktest.RequireEqual(t, positions, []compiler.Position{{File: "<test>", Line: 4}})
}

func TestDirective_CompactFormSplitsOnlyTopLevelCommas(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert: { result.text == "a,b", all(["a,b", "c"], # != ""), all([1, 2], # > 0), }`)
	ktest.RequireNoError(t, err)

	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts, []string{
		`result.text == "a,b"`,
		`all(["a,b", "c"], # != "")`,
		`all([1, 2], # > 0)`,
	})
	positions, _ := out[assertionPositionsKey].([]compiler.Position)
	ktest.RequireEqual(t, positions, []compiler.Position{
		{File: "<test>", Line: 1},
		{File: "<test>", Line: 1},
		{File: "<test>", Line: 1},
	})
}

func TestDirective_CompactFormRejectsEmptyListItems(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@assert: { a == 1,, b == 2 }`)
	ktest.RequireErrorContains(t, err, "empty expression")
}

func TestDirective_CompactFormAllowsNestedObjectAndCollection(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@assert: { { values: [1, 2] }.values[0] == 1, result.ok == true }`)
	ktest.RequireNoError(t, err)
	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts, []string{
		`{ values: [1, 2] }.values[0] == 1`,
		`result.ok == true`,
	})
}

func TestDirective_SingleExpressionStartingWithObjectLiteralIsPreserved(t *testing.T) {
	t.Parallel()
	expression := `{ value: "x" }.value == result.value`
	out, err := runDirective(t, "@assert: "+expression)
	ktest.RequireNoError(t, err)
	asserts, _ := out["asserts"].([]string)
	ktest.RequireEqual(t, asserts, []string{expression})
}

func TestDirective_BlockErrors(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		_, err := runDirective(t, `@assert {`, `# only a comment`, `}`)
		ktest.RequireErrorContains(t, err, "block must contain at least one expression")
	})
	t.Run("unclosed", func(t *testing.T) {
		t.Parallel()
		_, err := runDirective(t, `@assert {`, `a == 1`)
		ktest.RequireErrorContains(t, err, "unclosed block")
	})
}

func TestDirective_ValidatesExpressionSyntaxAndSourceLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		lines []string
		line  int
	}{
		{name: "single", lines: []string{`@assert: result.value >`}, line: 1},
		{name: "compact", lines: []string{`@assert: { result.value > 0, result.other == }`}, line: 1},
		{name: "block", lines: []string{`@assert {`, `result.value > 0`, `result.other ==`, `}`}, line: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := runDirective(t, tc.lines...)
			if err == nil {
				t.Fatal("directive accepted invalid expression syntax")
			}
			position, ok := compiler.PositionOf(err)
			if !ok || position.File != "<test>" || position.Line != tc.line {
				t.Fatalf("PositionOf(error) = %+v, %t; want <test>:%d (error: %v)", position, ok, tc.line, err)
			}
		})
	}
}

func TestDirective_ValidatesDotShorthandWithoutRuntimePayload(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@assert: .value > 0`)
	ktest.RequireNoError(t, err)
}
