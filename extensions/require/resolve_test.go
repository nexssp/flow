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
