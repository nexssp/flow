package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestFlags_ResolvesKnownFlag(t *testing.T) {
	t.Parallel()

	src := `const @{ value: "@flag.x" }`
	out, _, code := captureLintOutput(t, func() int {
		return runSourceInProcess(context.Background(), src, "inline", []string{"-x=42", "--json"})
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
