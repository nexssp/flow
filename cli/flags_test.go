package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestFlags_ResolvesKnownFlagAsDirectValue(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"42", "true", "null", `{"a":1}`} {
		out, _, code := captureLintOutput(t, func() int {
			return runSourceInProcess(context.Background(), `@flag.x`, "inline", []string{"-x=" + value, "--json"})
		})
		ktest.RequireEqual(t, code, 0)
		ktest.RequireEqual(t, strings.TrimSpace(out), strconv.Quote(value))
	}
}

func TestActionArgumentsResolveFlagsWithoutChangingStringType(t *testing.T) {
	t.Parallel()

	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), `runtime.noop(value: @flag.x)`, "inline", []string{"-x=42", "--json"})
	})
	ktest.RequireEqual(t, code, 0)
	ktest.RequireEqual(t, strings.TrimSpace(out), `{"value":"42"}`)
}

func TestDirectLiteralsKeepTheirDeclaredTypes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		source string
		want   string
	}{
		{source: `42`, want: "42"},
		{source: `"42"`, want: `"42"`},
		{source: `true`, want: "true"},
	} {
		out, stderr, code := captureLintOutput(t, func() int {
			return runSourceInProcess(context.Background(), tc.source, "inline", []string{"--json"})
		})
		if code != 0 {
			t.Fatalf("run %q exited with %d: %s", tc.source, code, stderr)
		}
		ktest.RequireEqual(t, strings.TrimSpace(out), tc.want)
	}
}

func TestPositionalArgsRemainStrings(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"42", "true", "null", `{"a":1}`} {
		out, _, code := captureLintOutput(t, func() int {
			return runSourceInProcess(context.Background(), `@arg.1`, "inline", []string{value, "--json"})
		})
		ktest.RequireEqual(t, code, 0)
		ktest.RequireEqual(t, strings.TrimSpace(out), strconv.Quote(value))
	}
}

func TestActionArgumentsResolvePositionalArgsWithoutChangingStringType(t *testing.T) {
	t.Parallel()

	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), `runtime.noop(value: @arg.1)`, "inline", []string{"42", "--json"})
	})
	ktest.RequireEqual(t, code, 0)
	ktest.RequireEqual(t, strings.TrimSpace(out), `{"value":"42"}`)
}

func TestFlags_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	_, stderr, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), `@flag.y`, "inline", []string{"-x=42"})
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "unknown @flag.y")
	ktest.RequireStringContains(t, stderr, "available: x")
}

func TestFlags_RejectsWhenNoFlagsPassed(t *testing.T) {
	t.Parallel()

	_, stderr, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), `@flag.debug`, "inline", []string{"--json"})
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "unknown @flag.debug")
	ktest.RequireStringContains(t, stderr, "available: none")
}
