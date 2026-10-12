package symbols

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func runSymbols(tb testing.TB, lines []string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: lines,
		Body:  append([]string(nil), lines...),
		I:     0,
		Out:   out,
		File:  "symbols-test.nflow",
	})
	return out, err
}

func TestDirective_RejectsDuplicateSymbolName(t *testing.T) {
	_, err := runSymbols(t, []string{
		"@symbols route {",
		`  status: "approved"`,
		`  status: "review"`,
		"}",
	})
	ktest.RequireErrorContains(t, err, `duplicate symbol "status"`)
}

func TestDirective_RejectsInvalidSymbolName(t *testing.T) {
	_, err := runSymbols(t, []string{
		"@symbols route {",
		`  needs-review: "review"`,
		"}",
	})
	ktest.RequireErrorContains(t, err, `invalid symbol name "needs-review"`)
}

func TestDirective_AcceptsUniqueSymbols(t *testing.T) {
	out, err := runSymbols(t, []string{
		"@symbols route {",
		`  approved: "approved"`,
		`  review: "review"`,
		"}",
	})
	ktest.RequireNoError(t, err)
	namespaces, _ := out["symbols"].(map[string]compiler.SymbolNamespace)
	ktest.RequireEqual(t, namespaces["route"].Symbols["approved"], "approved")
	ktest.RequireEqual(t, namespaces["route"].Symbols["review"], "review")
}
