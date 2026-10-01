package config

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// runConfig feeds a sequence of lines to handleConfig and returns the
// accumulated meta["config"] map.
func runConfig(tb testing.TB, lines ...string) map[string]string {
	tb.Helper()
	out := map[string]any{}
	for next := 0; next < len(lines); {
		res, err := handleConfig(context.Background(), core.DirectiveReq{
			Lines: lines,
			I:     next,
			Out:   out,
			File:  "<test>",
		})
		ktest.RequireNoError(tb, err)
		next = res.Next
	}
	cfg, _ := out["config"].(map[string]string)
	return cfg
}

func TestConfig_SingleLine(t *testing.T) {
	t.Parallel()
	cfg := runConfig(t, `@config:strict=true`)
	ktest.RequireEqual(t, cfg["strict"], "true")
}

func TestConfig_QuotedValue(t *testing.T) {
	t.Parallel()
	cfg := runConfig(t, `@config:key="value"`)
	ktest.RequireEqual(t, cfg["key"], "value")
}

func TestConfig_InlineBlock(t *testing.T) {
	t.Parallel()
	cfg := runConfig(t, `@config { retries: 3, timeout: "10s" }`)
	ktest.RequireEqual(t, cfg["retries"], "3")
	ktest.RequireEqual(t, cfg["timeout"], "10s")
}

func TestConfig_MultiLineBlock(t *testing.T) {
	t.Parallel()
	cfg := runConfig(t,
		`@config {`,
		`  retries: 3`,
		`  timeout: "10s"`,
		`}`,
	)
	ktest.RequireEqual(t, cfg["retries"], "3")
	ktest.RequireEqual(t, cfg["timeout"], "10s")
}

func TestConfig_BlockStripsInlineModifiers(t *testing.T) {
	t.Parallel()
	cfg := runConfig(t,
		`@config {`,
		`  retries: 3     :cli="r,retries" :desc="Max attempts"`,
		`}`,
	)
	ktest.RequireEqual(t, cfg["retries"], "3")
}

func TestConfig_Accumulates(t *testing.T) {
	t.Parallel()
	cfg := runConfig(t,
		`@config:strict=true`,
		`@config:silent=coverage`,
	)
	ktest.RequireEqual(t, cfg["strict"], "true")
	ktest.RequireEqual(t, cfg["silent"], "coverage")
}

func TestStripInlineModifiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{`3`, `3`},
		{`3 :cli="r"`, `3`},
		{`"10s" :cli="t"`, `"10s"`},
		{`"value : with colon"`, `"value : with colon"`},
		{`no modifiers`, `no modifiers`},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, stripInlineModifiers(c.in), c.want)
		})
	}
}
