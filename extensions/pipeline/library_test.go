package pipeline

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestBundle_WiresDirectiveMaterializeFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Directives), 2)
	ktest.RequireEqual(t, b.Directives[0].Name, "pipeline")
	ktest.RequireCondition(t, b.Materialize != nil, "Materialize is nil")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}

func TestDirective_SingleLineHeader(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	body := []string{`@pipeline p`, `  runtime.noop`, `@end`}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: body, Body: body, I: 0, Out: out, File: "<test>",
	})
	ktest.RequireNoError(t, err)

	pipelines, ok := out["pipelines"].(map[string]string)
	ktest.RequireCondition(t, ok, "pipelines = %T, want map[string]string", out["pipelines"])
	ktest.RequireEqual(t, pipelines["p"], "  runtime.noop")
}

func TestDirective_IgnoresInlineModifiers(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	body := []string{`@pipeline p:tag="fast"`, `  runtime.noop`, `@end`}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: body, Body: body, I: 0, Out: out, File: "<test>",
	})
	ktest.RequireNoError(t, err)

	pipelines, ok := out["pipelines"].(map[string]string)
	ktest.RequireCondition(t, ok, "pipelines = %T, want map[string]string", out["pipelines"])
	_, exists := pipelines["p"]
	ktest.RequireCondition(t, exists, "pipeline %q not stored", "p")
}

func TestDirective_MissingName(t *testing.T) {
	t.Parallel()
	body := []string{`@pipeline`, `  runtime.noop`, `@end`}
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: body, Body: body, I: 0, Out: out, File: "<test>",
	})
	ktest.RequireErrorContains(t, err, "requires a name")
}

func TestDirective_MultiplePipelines(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	lines := []string{
		`@pipeline a`,
		`  runtime.noop`,
		`@end`,
		`@pipeline b`,
		`  runtime.debug`,
		`@end`,
	}
	next := 0
	for next < len(lines) {
		res, err := handleDirective(context.Background(), core.DirectiveReq{
			Lines: lines, Body: lines, I: next, Out: out, File: "<test>",
		})
		ktest.RequireNoError(t, err)
		next = res.Next
	}

	pipelines, ok := out["pipelines"].(map[string]string)
	ktest.RequireCondition(t, ok, "pipelines = %T, want map[string]string", out["pipelines"])
	ktest.RequireEqual(t, len(pipelines), 2)
}

func TestFixtures_Discoverable(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	var count int
	walkErr := fs.WalkDir(b.Fixtures, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".nflow") {
			return nil
		}
		content, readErr := fs.ReadFile(b.Fixtures, p)
		ktest.RequireNoError(t, readErr)
		ktest.RequireCondition(t, strings.Contains(string(content), "@description"),
			"fixture %q has no @description", p)
		count++
		return nil
	})
	ktest.RequireNoError(t, walkErr)
	ktest.RequireCondition(t, count > 0, "no fixtures embedded")
}

func TestEndDirective_RejectsStrayEnd(t *testing.T) {
	t.Parallel()
	_, err := handleEnd(context.Background(), core.DirectiveReq{
		Lines: []string{"@end"},
		I:     0,
		File:  "stray.nflow",
	})
	ktest.RequireErrorContains(t, err, "@end without a matching block directive")
	ktest.RequireErrorContains(t, err, "stray.nflow:1")
}
