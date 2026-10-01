package config

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectivesAndPreprocess(t *testing.T) {
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(b.Directives), 2)
	ktest.RequireCondition(t, b.OnPreprocess != nil, "OnPreprocess is nil")
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestForwardConfigToCompiler_Empty(t *testing.T) {
	contribs := forwardConfigToCompiler(map[string]any{})
	ktest.RequireEqual(t, len(contribs.CompileOpts), 0)
}

func TestForwardConfigToCompiler_WithValues(t *testing.T) {
	meta := map[string]any{"config": map[string]string{"strict": "true"}}
	contribs := forwardConfigToCompiler(meta)
	ktest.RequireEqual(t, len(contribs.CompileOpts), 1)
}

func TestMetaConfig_CreatesAndReuses(t *testing.T) {
	out := map[string]any{}
	first := metaConfig(out)
	first["a"] = "1"
	second := metaConfig(out)
	ktest.RequireEqual(t, second["a"], "1")
}

func TestSelfTestShape(t *testing.T) {
	sections := Bundle(nil).SelfTest()
	ktest.RequireCondition(t, len(sections) > 0, "no sections")

	seen := make(map[string]bool)
	for _, s := range sections {
		ktest.RequireCondition(t, s.Name != "", "empty section name")
		for _, f := range s.Features {
			ktest.RequireCondition(t, f.Name != "", "feature has empty Name")
			ktest.RequireCondition(t, f.DSL != "", "feature %q has empty DSL", f.Name)
			ktest.RequireCondition(t, !seen[f.Name], "duplicate Name %q", f.Name)
			seen[f.Name] = true
		}
	}
}
