package require

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func TestResolveBundles_RejectsUnknownOption(t *testing.T) {
	factory := func(_ map[string]string) compiler.Bundle {
		return compiler.Bundle{
			ID:              "testbundle",
			AcceptedOptions: []string{"known"},
		}
	}
	compiler.Register("testbundle_opt_validation", factory)

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
	compiler.Register("testbundle_immediate_adoption", func(_ map[string]string) compiler.Bundle {
		return compiler.Bundle{
			ID:              "testbundle_immediate_adoption",
			AcceptedOptions: []string{},
		}
	})
	_, err := ResolveBundlesWith([]Requirement{{
		Import:  "testbundle_immediate_adoption",
		Options: map[string]string{"unknown": "x"},
	}}, func(bundle compiler.Bundle) {
		adopted = append(adopted, bundle.ID)
	})
	ktest.RequireCondition(t, err != nil, "expected option validation error")
	ktest.RequireStringContains(t, err.Error(), "unknown option")
	ktest.RequireEqual(t, len(adopted), 1)
	ktest.RequireEqual(t, adopted[0], "testbundle_immediate_adoption")
}

func TestResolveBundlesWith_RetainsEarlierBundlesWhenLaterRequirementIsMissing(t *testing.T) {
	var adopted []string
	compiler.Register("testbundle_partial_resolution", func(_ map[string]string) compiler.Bundle {
		return compiler.Bundle{ID: "testbundle_partial_resolution"}
	})
	_, err := ResolveBundlesWith([]Requirement{
		{Import: "testbundle_partial_resolution"},
		{Import: "testbundle_not_registered"},
	}, func(bundle compiler.Bundle) {
		adopted = append(adopted, bundle.ID)
	})
	ktest.RequireCondition(t, err != nil, "expected missing requirement error")
	ktest.RequireStringContains(t, err.Error(), "not registered")
	ktest.RequireEqual(t, len(adopted), 1)
	ktest.RequireEqual(t, adopted[0], "testbundle_partial_resolution")
}
