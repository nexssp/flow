package native_test

import (
	"slices"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

const extensionPrefix = "github.com/nexssp/flow/extensions/"

func TestPlanBuildAllMatchesStandardBundles(t *testing.T) {
	got, err := native.PlanBuild(nil, nil, true)
	ktest.RequireNoError(t, err)
	bundles := native.Bundles()
	ktest.RequireEqual(t, len(got), len(bundles))
	for i, bundle := range bundles {
		ktest.RequireEqual(t, got[i].ID, bundle.ID)
		ktest.RequireEqual(t, got[i].ImportPath, extensionPrefix+bundle.ID)
	}
}

func TestPlanBuildSelectsProgressAndActualDirectiveAndSourceTokens(t *testing.T) {
	got, err := native.PlanBuild(nil, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, idsOf(got), []string{"runtime", "progress"})

	got, err = native.PlanBuild([]compiler.BuildSource{{Directives: []string{"approval"}}}, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, slices.Contains(idsOf(got), "approval"), "executed @approval should select approval")

	got, err = native.PlanBuild([]compiler.BuildSource{{Raw: "@pipeline child { -> fs.read }", Directives: []string{"pipeline"}}}, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, slices.Contains(idsOf(got), "pipeline"), "pipeline directive should select pipeline")
	ktest.RequireCondition(t, slices.Contains(idsOf(got), "fs"), "child source action token should select fs")
}

func TestPlanBuildOptionalExtensionsRequireExplicitRequirement(t *testing.T) {
	got, err := native.PlanBuild([]compiler.BuildSource{{Raw: `"cov.items resilience template"`}}, nil, false)
	ktest.RequireNoError(t, err)
	for _, optional := range []string{"selftestkit", "resilience", "template", "config_yaml", "decide"} {
		ktest.RequireCondition(t, !slices.Contains(idsOf(got), optional), "optional bundle %q must not auto-activate", optional)
	}

	metaSource := compiler.BuildSource{Metadata: map[string]any{
		"require": []require.Requirement{{Import: extensionPrefix + "selftestkit"}},
	}}
	got, err = native.PlanBuild([]compiler.BuildSource{metaSource}, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, slices.Contains(idsOf(got), "selftestkit"), "metadata @require should select optional bundle")
}

func TestPlanBuildResilienceReplacementAndExplicitConflict(t *testing.T) {
	source := compiler.BuildSource{
		Raw: "service:retry=3",
		Metadata: map[string]any{
			"require": []require.Requirement{{Import: extensionPrefix + "resilience"}},
		},
	}
	got, err := native.PlanBuild([]compiler.BuildSource{source}, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, slices.Contains(idsOf(got), "resilience"), "explicit resilience requirement should select it")
	ktest.RequireCondition(t, !slices.Contains(idsOf(got), "retry"), "resilience replaces retry")

	_, err = native.PlanBuild(nil, []require.Requirement{
		{Import: extensionPrefix + "retry"},
		{Import: extensionPrefix + "resilience"},
	}, false)
	ktest.RequireErrorContains(t, err, "@require retry conflicts with @require resilience")
}

func idsOf(bundles []native.BuildBundle) []string {
	ids := make([]string, len(bundles))
	for i := range bundles {
		ids[i] = bundles[i].ID
	}
	return ids
}

func TestPlanBuildKeepsExactShortOptionalRequirements(t *testing.T) {
	for _, id := range []string{"resilience", "selftestkit"} {
		plan, err := native.PlanBuild(nil, []require.Requirement{{Import: id}}, false)
		ktest.RequireNoError(t, err)
		found := false
		for _, bundle := range plan {
			if bundle.ID == id {
				found = true
			}
		}
		ktest.RequireCondition(t, found, "explicit short @require %s was lost", id)
	}
}

func TestPlanBuildIncludesCanonicalGrammarBootProviders(t *testing.T) {
	plan, err := native.PlanBuild([]compiler.BuildSource{{Raw: "{} -> external.echo"}}, nil, false)
	ktest.RequireNoError(t, err)
	chosen := make(map[string]bool, len(plan))
	for _, entry := range plan {
		chosen[entry.ID] = true
	}
	var bundles []compiler.Bundle
	defaults := native.Bundles()
	for i := range defaults {
		if chosen[defaults[i].ID] {
			bundles = append(bundles, defaults[i])
		}
	}
	_, err = runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)
}

func TestPlanBuildPropagatesRequirementMetadataErrors(t *testing.T) {
	_, err := native.PlanBuild([]compiler.BuildSource{{Metadata: map[string]any{"require": "invalid"}}}, nil, false)
	ktest.RequireErrorContains(t, err, "require metadata has type string")

	_, err = native.PlanBuild(nil, []require.Requirement{{}}, false)
	ktest.RequireErrorContains(t, err, "empty import and package path")
}
