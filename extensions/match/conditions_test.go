package match

import (
	"strconv"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
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

func TestInitRegistersXerrNamespace(t *testing.T) {
	t.Parallel()
	value, ok := compiler.LookupSymbol("xerr", "KindTimeout")
	ktest.RequireCondition(t, ok, "xerr namespace was not registered by init()")
	ktest.RequireEqual(t, value, any(xerr.KindTimeout))
}

func TestWithSymbolEnvironment_BindsRegisteredNamespaces(t *testing.T) {
	compiler.RegisterNamespace(compiler.SymbolNamespace{
		Name:    "symenv_test",
		Symbols: map[string]any{"role": "billing.refund"},
	})
	env := WithSymbolEnvironment(t.Context(), map[string]any{
		"error": map[string]any{"kind": xerr.KindTimeout},
	})
	ns, ok := env["symenv_test"].(map[string]any)
	ktest.RequireCondition(t, ok, "symenv_test not bound as map: %T", env["symenv_test"])
	ktest.RequireEqual(t, ns["role"], "billing.refund")
}

func TestWithSymbolEnvironment_BindsXerrTyped(t *testing.T) {
	t.Parallel()
	env := WithSymbolEnvironment(t.Context(), map[string]any{
		"error": map[string]any{"kind": xerr.KindTimeout},
	})
	xerrMap, ok := env["xerr"].(map[string]any)
	ktest.RequireCondition(t, ok, "xerr not bound as map: %T", env["xerr"])
	ktest.RequireEqual(t, xerrMap["KindTimeout"], any(xerr.KindTimeout))
}

func TestWithSymbolEnvironment_BindsXerrString(t *testing.T) {
	t.Parallel()
	env := WithSymbolEnvironment(t.Context(), map[string]any{
		"error": map[string]any{"kind": "Timeout"},
	})
	xerrMap, ok := env["xerr"].(map[string]any)
	ktest.RequireCondition(t, ok, "xerr not bound as map: %T", env["xerr"])
	ktest.RequireEqual(t, xerrMap["KindTimeout"], "Timeout")
}

func TestWithSymbolEnvironment_CallerKeyWins(t *testing.T) {
	compiler.RegisterNamespace(compiler.SymbolNamespace{
		Name:    "symenv_caller",
		Symbols: map[string]any{"role": "from_registry"},
	})
	env := WithSymbolEnvironment(t.Context(), map[string]any{
		"symenv_caller": "caller_value",
	})
	ktest.RequireEqual(t, env["symenv_caller"], "caller_value")
}

func TestWithKindEnvironment_MatchesSymbolEnvironment(t *testing.T) {
	t.Parallel()
	in := map[string]any{"error": map[string]any{"kind": xerr.KindTimeout}}
	ktest.RequireEqual(t, WithKindEnvironment(in), WithSymbolEnvironment(t.Context(), in))
}

func TestWithSymbolEnvironment_BindsFileLocal(t *testing.T) {
	t.Parallel()
	ctx := compiler.WithLocalSymbols(t.Context(), map[string]compiler.SymbolNamespace{
		"filelocal": {Name: "filelocal", Symbols: map[string]any{"role": "from_file"}},
	})
	env := WithSymbolEnvironment(ctx, map[string]any{})
	ns, ok := env["filelocal"].(map[string]any)
	ktest.RequireCondition(t, ok, "filelocal not bound as map: %T", env["filelocal"])
	ktest.RequireEqual(t, ns["role"], "from_file")
}

func TestSubjectRelativeRelationalConditions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		condition string
		value     int
		want      bool
	}{
		{"> 90", 91, true},
		{"> 90", 90, false},
		{">= 50", 50, true},
		{">= 50", 49, false},
		{"< 50", 49, true},
		{"< 50", 50, false},
		{"<= 50", 50, true},
		{"<= 50", 51, false},
	} {
		t.Run(tc.condition+"/"+strconv.Itoa(tc.value), func(t *testing.T) {
			program, err := CompileCondition(matchConditionExpression(".peak", tc.condition))
			if err != nil {
				t.Fatalf("compile %q: %v", tc.condition, err)
			}
			got, err := EvaluateProgram(program, map[string]any{"__match_subject__": tc.value})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSubjectRelativeInvalidRelationalExpression(t *testing.T) {
	t.Parallel()
	for _, condition := range []string{">", ">= nope ???", "< ("} {
		if _, err := CompileCondition(matchConditionExpression(".peak", condition)); err == nil {
			t.Errorf("CompileCondition(%q) succeeded; want invalid-expression error", condition)
		}
	}
}

func TestSubjectMatchEqualityCompatibility(t *testing.T) {
	t.Parallel()
	program, err := CompileCondition(matchConditionExpression(".status", `"ACTIVE"`))
	if err != nil {
		t.Fatalf("compile equality condition: %v", err)
	}
	got, err := EvaluateProgram(program, map[string]any{"__match_subject__": "ACTIVE"})
	if err != nil || got != true {
		t.Fatalf("legacy equality result = %v, %v; want true, nil", got, err)
	}
	if got := matchConditionExpression("", ".ok"); got != ".ok" {
		t.Fatalf("subjectless match changed condition to %q", got)
	}
}
