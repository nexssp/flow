package require

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(tb testing.TB, lines ...string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, err
}

func TestDirective_BareModule(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@require example.com/foo`)
	ktest.RequireNoError(t, err)

	requirements, _ := out["require"].([]Requirement)
	ktest.RequireEqual(t, len(requirements), 1)
	ktest.RequireEqual(t, requirements[0].Import, "example.com/foo")
	ktest.RequireEqual(t, requirements[0].Version, "")
}

func TestDirective_WithVersion(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@require example.com/foo v1.2.3`)
	ktest.RequireNoError(t, err)

	requirements, _ := out["require"].([]Requirement)
	ktest.RequireEqual(t, requirements[0].Version, "v1.2.3")
}

func TestDirective_InlineOptions(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@require example.com/foo { id: "custom", cmd: "python3 x.py" }`)
	ktest.RequireNoError(t, err)

	requirements, _ := out["require"].([]Requirement)
	ktest.RequireEqual(t, requirements[0].Options["id"], "custom")
	ktest.RequireEqual(t, requirements[0].Options["cmd"], "python3 x.py")
}

func TestDirective_BlockOptions(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@require example.com/foo`,
		`{`,
		`  id: "custom"`,
		`}`,
	)
	ktest.RequireNoError(t, err)

	requirements, _ := out["require"].([]Requirement)
	ktest.RequireEqual(t, requirements[0].Options["id"], "custom")
}

func TestDirective_DuplicateCollapsed(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	lines := []string{`@require example.com/foo v1.0.0`, `@require example.com/foo v1.0.0`}
	for _, line := range lines {
		_, err := handleDirective(context.Background(), core.DirectiveReq{
			Lines: []string{line}, I: 0, Out: out, File: "<test>",
		})
		ktest.RequireNoError(t, err)
	}
	requirements, _ := out["require"].([]Requirement)
	ktest.RequireEqual(t, len(requirements), 1)
}

func TestDirective_MissingModule(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@require`)
	ktest.RequireErrorContains(t, err, "missing module")
}

func TestDirective_VersionWithoutV(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@require example.com/foo 1.0.0`)
	ktest.RequireErrorContains(t, err, "needs a version")
}

func TestDirective_TooManyArgs(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@require example.com/foo v1.0.0 extra`)
	ktest.RequireErrorContains(t, err, "extraneous tokens")
}
