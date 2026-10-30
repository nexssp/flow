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

func TestDirective_AcceptsBareNames(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		lines []string
	}{
		{
			name:  "comma-free",
			lines: []string{"@symbols memory { search put chat_turn }"},
		},
		{
			name:  "comma-separated",
			lines: []string{"@symbols memory { search, put, chat_turn }"},
		},
		{
			name: "mixed with explicit values",
			lines: []string{
				"@symbols memory {",
				"search put,",
				`chat_turn: "TURN"`,
				"}",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			out, err := runSymbols(t, test.lines)
			ktest.RequireNoError(t, err)
			namespaces, _ := out["symbols"].(map[string]compiler.SymbolNamespace)
			ktest.RequireEqual[any](t, namespaces["memory"].Symbols["search"], "search")
			ktest.RequireEqual[any](t, namespaces["memory"].Symbols["put"], "put")
			wantTurn := "chat_turn"
			if test.name == "mixed with explicit values" {
				wantTurn = "TURN"
			}
			ktest.RequireEqual[any](t, namespaces["memory"].Symbols["chat_turn"], wantTurn)
		})
	}
}

func TestDirective_RejectsDuplicateBareName(t *testing.T) {
	t.Parallel()
	_, err := runSymbols(t, []string{"@symbols memory { search put search }"})
	ktest.RequireErrorContains(t, err, `duplicate symbol "search"`)
}
