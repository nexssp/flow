package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestFlags_ResolvesKnownFlag(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "direct scalar", source: `const @flag.x`},
		{name: "named reference", source: `const(value: @flag.x) -> pick(field: "value")`},
		{name: "quoted reference", source: `const(value: "@flag.x") -> pick(field: "value")`},
		{name: "legacy parameter", source: `const(value=@flag.x)`},
		{name: "legacy quoted parameter", source: `const(value="@flag.x")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, value := range []string{"42", "true", "null", `{"a":1}`} {
				out, _, code := captureLintOutput(t, func() int {
					return runSourceInProcess(context.Background(), tc.source, "inline", []string{"-x=" + value, "--json"})
				})
				ktest.RequireEqual(t, code, 0)
				ktest.RequireEqual(t, strings.TrimSpace(out), strconv.Quote(value))
			}
		})
	}
}

func TestFlagsNamedArgumentsPreserveRawStringType(t *testing.T) {
	t.Parallel()

	src := `const(value: @flag.x, coerce: true)`
	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"-x=42", "--json"})
	})
	ktest.RequireEqual(t, code, 0)
	ktest.RequireEqual(t, strings.TrimSpace(out), `{"coerce":true,"value":"42"}`)
}

func TestLegacyConstKeyValueMagicParameters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		args   []string
		want   string
	}{
		{name: "value unwraps and parses literal", source: `const(value=42)`, want: "42"},
		{name: "val alias unwraps and parses literal", source: `const(val=42)`, want: "42"},
		{name: "value with text coerce flag preserves flag string", source: `const(value="@flag.x", coerce=true)`, args: []string{"-x=42"}, want: `"42"`},
		{name: "val with text coerce flag preserves flag string", source: `const(val="@flag.x", coerce=true)`, args: []string{"-x=42"}, want: `"42"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append([]string(nil), tc.args...), "--json")
			out, stderr, code := captureLintOutput(t, func() int {
				return runSourceInProcess(context.Background(), tc.source, "inline", args)
			})
			if code != 0 {
				t.Fatalf("run exited with %d: %s", code, stderr)
			}
			ktest.RequireEqual(t, code, 0)
			ktest.RequireEqual(t, strings.TrimSpace(out), tc.want)
		})
	}
}

func TestPositionalArgsRemainStrings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "direct scalar", source: `const @arg.1`},
		{name: "named reference", source: `const(value: @arg.1) -> pick(field: "value")`},
		{name: "quoted reference", source: `const "@arg.1"`},
		{name: "legacy parameter", source: `const(value=@arg.1)`},
		{name: "legacy quoted parameter", source: `const(value="@arg.1")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, value := range []string{"42", "true", "null", `{"a":1}`} {
				out, _, code := captureLintOutput(t, func() int {
					return runSourceInProcess(context.Background(), tc.source, "inline", []string{value, "--json"})
				})
				ktest.RequireEqual(t, code, 0)
				ktest.RequireEqual(t, strings.TrimSpace(out), strconv.Quote(value))
			}
		})
	}
}

func TestPositionalNamedArgumentsPreserveRawStringType(t *testing.T) {
	t.Parallel()

	src := `const(value: @arg.1, coerce: true)`
	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"42", "--json"})
	})
	ktest.RequireEqual(t, code, 0)
	ktest.RequireEqual(t, strings.TrimSpace(out), `{"coerce":true,"value":"42"}`)
}

func TestFlags_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	src := `const(value: "@flag.y") -> pick(field: "value")`
	_, stderr, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"-x=42"})
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "unknown @flag.y")
	ktest.RequireStringContains(t, stderr, "available: x")
}

func TestFlags_RejectsWhenNoFlagsPassed(t *testing.T) {
	t.Parallel()

	src := `const(value: "@flag.debug") -> pick(field: "value")`
	_, stderr, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"--json"})
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "unknown @flag.debug")
	ktest.RequireStringContains(t, stderr, "available: none")
}
