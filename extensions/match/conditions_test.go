package match

import (
	"testing"

	"github.com/nexssp/kernel/xerr"
)

func TestKindSymbolsResolveAndMatchEveryBuiltIn(t *testing.T) {
	t.Parallel()

	for _, kind := range xerr.AllKinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			symbol := KindSymbol(kind)
			resolved, ok := ResolveKindSymbol(symbol)
			if !ok || resolved != kind {
				t.Fatalf("ResolveKindSymbol(%q) = %q, %v; want %q, true", symbol, resolved, ok, kind)
			}

			program, err := CompileCondition("error.kind == " + symbol)
			if err != nil {
				t.Fatalf("compile %q: %v", symbol, err)
			}
			env := WithKindEnvironment(map[string]any{
				"error": map[string]any{"kind": kind},
			})
			if got := FirstMatchingCase([]ConditionCase{{Program: program}}, env); got != 0 {
				t.Fatalf("typed symbol %q did not match its kind; selected %d", symbol, got)
			}
		})
	}
}

func TestCustomKindFallsThroughToElseAndIsNotClosed(t *testing.T) {
	t.Parallel()

	if _, ok := ResolveKindSymbol("xerr.KindVendorSpecific"); ok {
		t.Fatal("custom kinds must not resolve as built-in symbols")
	}
	program, err := CompileCondition("error.kind == xerr.KindTimeout")
	if err != nil {
		t.Fatalf("compile built-in condition: %v", err)
	}
	env := WithKindEnvironment(map[string]any{
		"error": map[string]any{"kind": xerr.Kind("VendorSpecific")},
	})
	cases := []ConditionCase{{Program: program}, {IsDefault: true}}
	if got := FirstMatchingCase(cases, env); got != 1 {
		t.Fatalf("custom kind selected case %d, want explicit else at index 1", got)
	}
	if got := FirstMatchingCase(cases[:1], env); got != -1 {
		t.Fatalf("custom kind without else selected case %d, want no match", got)
	}
}

func TestStringConditionRemainsCompatible(t *testing.T) {
	t.Parallel()

	program, err := CompileCondition(`error.kind == "Timeout"`)
	if err != nil {
		t.Fatalf("compile legacy string condition: %v", err)
	}
	env := WithKindEnvironment(map[string]any{
		"error": map[string]any{"kind": "Timeout"},
	})
	if got := FirstMatchingCase([]ConditionCase{{Program: program}}, env); got != 0 {
		t.Fatalf("legacy string condition selected %d, want 0", got)
	}
}

func TestLegacyStringKindCanMatchKernelSymbol(t *testing.T) {
	t.Parallel()

	program, err := CompileCondition("error.kind == xerr.KindTimeout")
	if err != nil {
		t.Fatalf("compile symbolic condition: %v", err)
	}
	env := WithKindEnvironment(map[string]any{
		"error": map[string]any{"kind": "Timeout"},
	})
	if got := FirstMatchingCase([]ConditionCase{{Program: program}}, env); got != 0 {
		t.Fatalf("legacy string kind did not match xerr.KindTimeout; selected %d", got)
	}
}
