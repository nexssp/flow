package core_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/nodes_distribute"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func buildConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		nodes_distribute.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func TestCapabilityRef_ValidKeyword(t *testing.T) {
	cfg := buildConfig(t)
	src := `distribute.map @{ action: noop, items: [1] }`
	_, err := runner.Execute(context.Background(), cfg, src, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
}

func TestCapabilityRef_ValidCanonical(t *testing.T) {
	cfg := buildConfig(t)
	src := `distribute.map @{ action: runtime.noop, items: [1] }`
	_, err := runner.Execute(context.Background(), cfg, src, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
}

func TestCapabilityRef_RejectsStringLiteral(t *testing.T) {
	cfg := buildConfig(t)
	src := `distribute.map @{ action: "noop", items: [1] }`
	_, err := runner.Execute(context.Background(), cfg, src, "bad.nflow", nil)
	ktest.RequireCondition(t, err != nil, "expected error for quoted capability ref")
	ktest.RequireStringContains(t, err.Error(), "bare capability reference")
}

func TestCapabilityRef_RejectsUnknown(t *testing.T) {
	cfg := buildConfig(t)
	src := `distribute.map @{ action: nope, items: [1] }`
	_, err := runner.Execute(context.Background(), cfg, src, "bad.nflow", nil)
	ktest.RequireCondition(t, err != nil, "expected error for unknown capability")
	ktest.RequireStringContains(t, err.Error(), "unknown capability")
}

func TestCapabilityRef_StringValueStaysData(t *testing.T) {
	cfg := buildConfig(t)
	// runtime.const has no ArgSchema, so the quoted "noop" is data and
	// must never resolve as an action.
	src := `const @{ value: "noop" }`
	ex, err := runner.Execute(context.Background(), cfg, src, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "noop")
}

func TestBareToken_NonSchemaFieldPreservesString(t *testing.T) {
	cfg := buildConfig(t)
	src := `const @{ value: noop }`
	ex, err := runner.Execute(context.Background(), cfg, src, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "noop")
}

func TestBareToken_KeywordIsNotAutoResolved(t *testing.T) {
	cfg := buildConfig(t)
	// `value` is not declared as ArgCapabilityRef on runtime.const,
	// so the bare token "const" is data, not a self-reference.
	src := `const @{ value: const }`
	ex, err := runner.Execute(context.Background(), cfg, src, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "const")
}
