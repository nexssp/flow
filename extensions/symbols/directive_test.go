package symbols

import (
	"context"
	"testing"
	"time"

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

func TestDirective_ExplicitTypedSymbolsKeepNativeValuesAndLegacyStrings(t *testing.T) {
	out, err := runSymbols(t, []string{
		"@symbols http_status {",
		`  created: int = 201; base_url: string = "https://api.internal"; timeout: duration = 5s; alias_timeout: time.Duration = 3s`,
		`  legacy: "201"; pending`,
		"}",
	})
	ktest.RequireNoError(t, err)
	namespaces, _ := out["symbols"].(map[string]compiler.SymbolNamespace)
	symbols := namespaces["http_status"].Symbols
	created, ok := symbols["created"].(compiler.TypedSymbolValue)
	ktest.RequireCondition(t, ok, "created must retain its typed-symbol carrier")
	ktest.RequireEqual(t, created.Type, "int")
	ktest.RequireEqual[any](t, created.Value, 201)
	baseURL, ok := symbols["base_url"].(compiler.TypedSymbolValue)
	ktest.RequireCondition(t, ok, "base_url must retain its typed-symbol carrier")
	ktest.RequireEqual(t, baseURL.Type, "string")
	ktest.RequireEqual[any](t, baseURL.Value, "https://api.internal")
	timeout, ok := symbols["timeout"].(compiler.TypedSymbolValue)
	ktest.RequireCondition(t, ok, "timeout must retain its typed-symbol carrier")
	ktest.RequireEqual(t, timeout.Type, "duration")
	ktest.RequireEqual[any](t, timeout.Value, 5*time.Second)
	aliasTimeout, ok := symbols["alias_timeout"].(compiler.TypedSymbolValue)
	ktest.RequireCondition(t, ok, "time.Duration must retain its typed-symbol carrier")
	ktest.RequireEqual(t, aliasTimeout.Type, "duration")
	ktest.RequireEqual[any](t, aliasTimeout.Value, 3*time.Second)
	ktest.RequireEqual[any](t, symbols["legacy"], "201")
	ktest.RequireEqual[any](t, symbols["pending"], "pending")
}

func TestDirective_RejectsTypedSymbolOverflowAndInvalidDurationAtLiteral(t *testing.T) {
	for _, test := range []struct {
		name     string
		typeName string
		value    string
		want     string
	}{
		{name: "integer overflow", typeName: "int", value: "9223372036854775808", want: "invalid int literal"},
		{name: "invalid duration", typeName: "duration", value: "5fortnights", want: "invalid duration literal"},
		{name: "unterminated typed string", typeName: "string", value: `"unterminated`, want: "double-quoted"},
		{name: "trailing typed string text", typeName: "string", value: `"ok"junk`, want: "double-quoted"},
		{name: "invalid typed string escape", typeName: "string", value: `"bad\q"`, want: "invalid string literal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := runSymbols(t, []string{
				"@symbols invalid {",
				"  value: " + test.typeName + " = " + test.value,
				"}",
			})
			ktest.RequireErrorContains(t, err, test.want)
			position, ok := compiler.PositionOf(err)
			ktest.RequireCondition(t, ok, "typed literal errors must be source-located")
			ktest.RequireEqual(t, position.Line, 2)
		})
	}
}
