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
		{name: "named reference", source: `const @{ value: @flag.x }`},
		{name: "quoted reference", source: `const @{ value: "@flag.x" }`},
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

func TestFlags_ExplicitlyCoercesKnownFlag(t *testing.T) {
	t.Parallel()

	src := `const @{ value: @flag.x, coerce: true }`
	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"-x=42", "--json"})
	})
	ktest.RequireEqual(t, code, 0)
	ktest.RequireEqual(t, strings.TrimSpace(out), "42")
}

func TestPositionalArgsRemainStrings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "direct scalar", source: `const @arg.1`},
		{name: "named reference", source: `const @{ value: @arg.1 }`},
		{name: "quoted reference", source: `const @{ value: "@arg.1" }`},
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

func TestPositionalArgs_ExplicitlyCoerce(t *testing.T) {
	t.Parallel()

	src := `const @{ value: @arg.1, coerce: true }`
	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"42", "--json"})
	})
	ktest.RequireEqual(t, code, 0)
	ktest.RequireEqual(t, strings.TrimSpace(out), "42")
}

func TestFlags_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	src := `const @{ value: "@flag.y" }`
	_, stderr, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"-x=42"})
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "unknown @flag.y")
	ktest.RequireStringContains(t, stderr, "available: x")
}

func TestFlags_RejectsWhenNoFlagsPassed(t *testing.T) {
	t.Parallel()

	src := `const @{ value: "@flag.debug" }`
	_, stderr, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"--json"})
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "unknown @flag.debug")
	ktest.RequireStringContains(t, stderr, "available: none")
}
