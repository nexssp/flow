package include

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// runInclude simulates the Recurse callback with a canned result. The
// real recursion is exercised through the runner; here we test only
// this directive's parsing and merge logic.
func runInclude(tb testing.TB, line, baseDir string, recurse func(string) (string, map[string]any, error)) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	body := []string{line}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines:   body,
		Body:    body,
		I:       0,
		Out:     out,
		File:    "<test>",
		BaseDir: baseDir,
		Recurse: recurse,
	})
	return out, err
}

func TestDirective_MergesPipelines(t *testing.T) {
	t.Parallel()
	out, err := runInclude(t, `@include "child.nflow"`, "", func(_ string) (string, map[string]any, error) {
		return `runtime.const @{ value: "x" }`, map[string]any{
			"pipelines": map[string]string{"p": `runtime.const @{ value: "x" }`},
		}, nil
	})
	ktest.RequireNoError(t, err)

	pipelines, _ := out["pipelines"].(map[string]string)
	ktest.RequireEqual(t, pipelines["p"], `runtime.const @{ value: "x" }`)
}

func TestDirective_ConcatenatesRequire(t *testing.T) {
	t.Parallel()
	out, err := runInclude(t, `@include "child.nflow"`, "", func(_ string) (string, map[string]any, error) {
		return "", map[string]any{"require": []string{"a", "b"}}, nil
	})
	ktest.RequireNoError(t, err)

	req, _ := out["require"].([]string)
	ktest.RequireEqual(t, req, []string{"a", "b"})
}

func TestDirective_ParentValueWins(t *testing.T) {
	t.Parallel()
	out := map[string]any{"description": "parent"}
	body := []string{`@include "child.nflow"`}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: body, Body: body, I: 0, Out: out, File: "<test>",
		Recurse: func(_ string) (string, map[string]any, error) {
			return "", map[string]any{"description": "child"}, nil
		},
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, out["description"], "parent")
}

func TestDirective_AbsolutePathPassthrough(t *testing.T) {
	t.Parallel()
	absolute := filepath.Join(t.TempDir(), "child.nflow")
	var captured string
	_, err := runInclude(t, `@include "`+absolute+`"`, "/some/base", func(p string) (string, map[string]any, error) {
		captured = p
		return "", nil, nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, captured, absolute)
}

func TestDirective_RelativeToBaseDir(t *testing.T) {
	t.Parallel()
	var captured string
	_, err := runInclude(t, `@include "child.nflow"`, "/tmp/base", func(p string) (string, map[string]any, error) {
		captured = p
		return "", nil, nil
	})
	ktest.RequireNoError(t, err)

	want, _ := filepath.Abs("/tmp/base/child.nflow")
	ktest.RequireEqual(t, captured, want)
}

func TestDirective_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include`, "", nil)
	ktest.RequireErrorContains(t, err, "requires a file path")
}

func TestDirective_NoRecurse(t *testing.T) {
	t.Parallel()
	_, err := runInclude(t, `@include "x.nflow"`, "", nil)
	ktest.RequireErrorContains(t, err, "recurse is not available")
}
