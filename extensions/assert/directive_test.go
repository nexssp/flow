package assert

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
		_, err := handleDirective(context.Background(), core.DirectiveReq{
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
