package require

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestResolveBundles_RejectsUnknownOption(t *testing.T) {
	factory := func(_ map[string]string) core.Bundle {
		return core.Bundle{
			ID:              "testbundle",
			AcceptedOptions: []string{"known"},
		}
	}
	core.Register("testbundle_opt_validation", factory)

	_, err := ResolveBundles([]Requirement{
		{
			Import:  "testbundle_opt_validation",
			Options: map[string]string{"unknown": "x"},
		},
	})
	ktest.RequireCondition(t, err != nil, "expected error for unknown option")
	ktest.RequireStringContains(t, err.Error(), "unknown option")
}

func TestResolveBundlesWith_AdoptsBeforeOptionValidation(t *testing.T) {
	var adopted []string
	core.Register("testbundle_immediate_adoption", func(_ map[string]string) core.Bundle {
		return core.Bundle{
			ID:              "testbundle_immediate_adoption",
			AcceptedOptions: []string{},
		}
	})
	_, err := ResolveBundlesWith([]Requirement{{
		Import:  "testbundle_immediate_adoption",
		Options: map[string]string{"unknown": "x"},
	}}, func(bundle core.Bundle) {
		adopted = append(adopted, bundle.ID)
	})
	ktest.RequireCondition(t, err != nil, "expected option validation error")
	ktest.RequireStringContains(t, err.Error(), "unknown option")
	ktest.RequireEqual(t, len(adopted), 1)
	ktest.RequireEqual(t, adopted[0], "testbundle_immediate_adoption")
}

func TestResolveBundlesWith_RetainsEarlierBundlesWhenLaterRequirementIsMissing(t *testing.T) {
	var adopted []string
	core.Register("testbundle_partial_resolution", func(_ map[string]string) core.Bundle {
		return core.Bundle{ID: "testbundle_partial_resolution"}
	})
	_, err := ResolveBundlesWith([]Requirement{
		{Import: "testbundle_partial_resolution"},
		{Import: "testbundle_not_registered"},
	}, func(bundle core.Bundle) {
		adopted = append(adopted, bundle.ID)
	})
	ktest.RequireCondition(t, err != nil, "expected missing requirement error")
	ktest.RequireStringContains(t, err.Error(), "not registered")
	ktest.RequireEqual(t, len(adopted), 1)
	ktest.RequireEqual(t, adopted[0], "testbundle_partial_resolution")
}
