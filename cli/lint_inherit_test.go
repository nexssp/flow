package cli

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/scope"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func inheritLintConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		projection.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		scope.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func lintSource(t *testing.T, src string) []LintIssue {
	t.Helper()
	cfg := inheritLintConfig(t)
	known, modifiers := registrySurface(cfg)
	return lintFile("test.nflow", src, cfg, known, modifiers)
}

func TestLint_UnknownModifierInScope(t *testing.T) {
	issues := lintSource(t, `@scope :timetout=5s {
  noop
}`)
	ktest.RequireCondition(t, len(issues) >= 1, "expected at least one issue")
	found := false
	for _, i := range issues {
		if i.Kind == "unknown_modifier" {
			found = true
			ktest.RequireStringContains(t, i.Message, "timetout")
		}
	}
	ktest.RequireCondition(t, found, "expected unknown_modifier; got %+v", issues)
}

func TestLint_InvalidValueInProfile(t *testing.T) {
	issues := lintSource(t, `@profile fast :timeout=abc

@pipeline work :profile=fast
  noop
@end

{} -> pipeline.work`)
	ktest.RequireCondition(t, len(issues) >= 1, "expected at least one issue")
	found := false
	for _, i := range issues {
		if i.Kind == "invalid_modifier_value" {
			found = true
			ktest.RequireStringContains(t, i.Message, "duration")
		}
	}
	ktest.RequireCondition(t, found, "expected invalid_modifier_value; got %+v", issues)
}

func TestLint_InvalidValueOnPipeline(t *testing.T) {
	issues := lintSource(t, `@pipeline work :timeout=abc
  noop
@end

{} -> pipeline.work`)
	ktest.RequireCondition(t, len(issues) >= 1, "expected at least one issue")
	found := false
	for _, i := range issues {
		if i.Kind == "invalid_modifier_value" {
			found = true
		}
	}
	ktest.RequireCondition(t, found, "expected invalid_modifier_value; got %+v", issues)
}

func TestLint_ValidScopeAndProfileNoIssues(t *testing.T) {
	issues := lintSource(t, `@profile fast :timeout=5s

@pipeline work :profile=fast
  @scope :retry=2 {
    noop
  }
@end

{} -> pipeline.work`)
	ktest.RequireEqual(t, len(issues), 0)
}
