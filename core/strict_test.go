package core

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xtest/ktest"
)

// ── hasModifier ──────────────────────────────────────────────────────

func TestHasModifier_Matches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		mods  []string
		query string
		want  bool
	}{
		{"exact flag", []string{"strict"}, "strict", true},
		{"value form strips suffix", []string{"strict=true"}, "strict", true},
		{"value form with empty value", []string{"strict="}, "strict", true},
		{"case insensitive query lower", []string{"STRICT"}, "strict", true},
		{"case insensitive query upper", []string{"strict"}, "STRICT", true},
		{"case insensitive both", []string{"StRiCt"}, "sTrIcT", true},
		{"whitespace trimmed", []string{"  strict  "}, "strict", true},
		{"whitespace with value", []string{" strict = true "}, "strict", true},
		{"among others", []string{"timeout=5s", "strict", "debug"}, "strict", true},
		{"suffix collision", []string{"strict_x"}, "strict", false},
		{"prefix collision", []string{"x_strict"}, "strict", false},
		{"different name", []string{"lenient"}, "strict", false},
		{"empty list", nil, "strict", false},
		{"empty query never matches", []string{"strict"}, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hasModifier(tc.mods, tc.query)
			ktest.RequireEqual(t, got, tc.want)
		})
	}
}

// ── isStrictConfig ───────────────────────────────────────────────────

func TestIsStrictConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"no config key", map[string]any{}, false},
		{"config is nil", map[string]any{"config": nil}, false},
		{"config wrong type", map[string]any{"config": "strict=true"}, false},
		{"empty config", map[string]any{"config": map[string]string{}}, false},
		{"strict true", map[string]any{"config": map[string]string{"strict": "true"}}, true},
		{"strict false", map[string]any{"config": map[string]string{"strict": "false"}}, false},
		{"strict TRUE uppercase", map[string]any{"config": map[string]string{"strict": "TRUE"}}, true},
		{"strict True mixed", map[string]any{"config": map[string]string{"strict": "True"}}, true},
		{"strict with spaces", map[string]any{"config": map[string]string{"strict": "  true  "}}, true},
		{"strict yes is not true", map[string]any{"config": map[string]string{"strict": "yes"}}, false},
		{"strict 1 is not true", map[string]any{"config": map[string]string{"strict": "1"}}, false},
		{"unrelated config key", map[string]any{"config": map[string]string{"timeout": "5s"}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isStrictConfig(tc.meta)
			ktest.RequireEqual(t, got, tc.want)
		})
	}
}

// ── WithStrict / strictFromCtx round-trip ────────────────────────────

// foreignKey simulates any other xctx-typed value that might be added to
// a derived context — action.WithExecutionID, xctx.WithTraceID, a custom
// hook. It proves strict survives unrelated context derivation.
var foreignKey = xctx.NewKey[string]("test.foreign")

func TestStrictContext_RoundTrip(t *testing.T) {
	t.Parallel()

	base := context.Background()

	ktest.RequireCondition(t, !strictFromCtx(base),
		"fresh context must not be strict")

	strict := WithStrict(base)
	ktest.RequireCondition(t, strictFromCtx(strict),
		"WithStrict must mark context strict")

	// A derived context carrying an unrelated xctx value must keep the
	// strict flag.
	derived := foreignKey.With(strict, "unrelated")
	ktest.RequireCondition(t, strictFromCtx(derived),
		"strict flag must survive context derivation")

	// Nil-safe: a nil context must not be strict.
	//nolint:staticcheck // SA1012: nil context is exactly the case under test
	ktest.RequireCondition(t, !strictFromCtx(nil),
		"nil context must not be strict")
}
