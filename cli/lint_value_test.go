package cli

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestLint_RejectsInvalidModifierValue(t *testing.T) {
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		modifiers_core.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	known, modifiers := registrySurface(cfg)

	src := `noop:timeout=abc`
	issues := lintFragment("test.nflow", src, cfg, known, modifiers)

	ktest.RequireCondition(t, len(issues) >= 1, "expected at least one issue")
	found := false
	for _, issue := range issues {
		if issue.Kind == "invalid_modifier_value" {
			found = true
			ktest.RequireStringContains(t, issue.Message, "expected duration")
		}
	}
	ktest.RequireCondition(t, found, "expected invalid_modifier_value issue")
}

func TestLint_AcceptsValidModifierValue(t *testing.T) {
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		modifiers_core.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	known, modifiers := registrySurface(cfg)

	src := `noop:timeout=5s`
	issues := lintFragment("test.nflow", src, cfg, known, modifiers)
	ktest.RequireEqual(t, len(issues), 0)
}
