package core_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/nodes_dispatch"
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
		nodes_dispatch.Bundle(nil),
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

func TestDispatchRun_MembersMustBeDeclaredInAtomArgs(t *testing.T) {
	t.Parallel()
	cfg := buildConfig(t)
	src := `const @{ value: { members: [noop] } } -> dispatch.run`
	_, err := runner.Execute(context.Background(), cfg, src, "missing-members.nflow", nil)
	ktest.RequireCondition(t, err != nil, "expected missing members compile error")

	// The error carries file:line:col since Position.col was added. Split
	// the location assertion from the message assertion so a column
	// change does not break the contract this test guards.
	ktest.RequireStringContains(t, err.Error(), "missing-members.nflow:1:")
	ktest.RequireStringContains(t, err.Error(), `dispatch.run: field "members"`)
	ktest.RequireStringContains(t, err.Error(), "must be declared in the atom's @{ ... } block")
	ktest.RequireStringContains(t, err.Error(), "not supplied only through pipeline input")
	ktest.RequireStringContains(t, err.Error(), "hint: dispatch.run @{ members: [runtime.fail, runtime.const] }")
}

func TestDispatchRun_ValidMembersCompileAndRun(t *testing.T) {
	cfg := buildConfig(t)
	ex, err := runner.Execute(context.Background(), cfg,
		`dispatch.run @{ members: [noop], payload: { value: "ok" } }`, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
	output, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "dispatch output is not an object: %#v", ex.Output)
	ktest.RequireEqual(t, output["value"], "ok")
}

func TestDispatchRun_RejectsQuotedAndUnknownMembers(t *testing.T) {
	cfg := buildConfig(t)
	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "quoted", src: `dispatch.run @{ members: ["runtime.noop"] }`, want: "bare capability reference"},
		{name: "unknown", src: `dispatch.run @{ members: [not.registered] }`, want: "unknown capability"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runner.Execute(context.Background(), cfg, tc.src, "bad.nflow", nil)
			ktest.RequireCondition(t, err != nil, "expected compile error")
			ktest.RequireStringContains(t, err.Error(), tc.want)
		})
	}
}

func TestRuntimeCall_NameCanComeFromPipelineInput(t *testing.T) {
	cfg := buildConfig(t)
	schema := runtime.Bundle(nil).ArgSchemas["runtime.call"]
	ktest.RequireEqual(t, len(schema), 1)
	ktest.RequireCondition(t, schema[0].Optional, "runtime.call.name must remain optional for dynamic input")

	src := `const @{ value: { name: "runtime.const", payload: { value: "called dynamically" } } } -> runtime.call`
	ex, err := runner.Execute(context.Background(), cfg, src, "dynamic-call.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "called dynamically")
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

func TestModifierValue_DurationRejectedAtCompile(t *testing.T) {
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		modifiers_core.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	src := `noop:timeout=abc`
	_, err = runner.Execute(context.Background(), cfg, src, "bad.nflow", nil)
	ktest.RequireCondition(t, err != nil, "expected compile-time error")
	ktest.RequireStringContains(t, err.Error(), "expected duration")
}

func TestModifierValue_IntRejectedAtCompile(t *testing.T) {
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		modifiers_core.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	src := `noop:retry=abc`
	_, err = runner.Execute(context.Background(), cfg, src, "bad.nflow", nil)
	ktest.RequireCondition(t, err != nil, "expected compile-time error")
	ktest.RequireStringContains(t, err.Error(), "expected integer")
}
