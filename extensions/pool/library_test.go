package pool

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestBundle_WiresDirectiveMaterializeFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "pool")
	ktest.RequireCondition(t, b.Materialize != nil, "Materialize is nil")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}

func TestPoolAction_CanBeCalledDirectly(t *testing.T) {
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtimeext.Bundle(nil),
		Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	ex, err := runner.Execute(context.Background(), cfg,
		"@pool workers [noop]\npool.workers @{ value: \"p\" }",
		"pool-direct.nflow", nil)
	ktest.RequireNoError(t, err)
	output, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "pool output is not an object: %#v", ex.Output)
	ktest.RequireEqual(t, output["value"], "p")
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
