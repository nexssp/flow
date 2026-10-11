package selftest

import (
	"testing"

	core "github.com/nexssp/flow/compiler"
)

func TestSectionsFromBundlesPreservesFeatureDescriptions(t *testing.T) {
	bundle := core.Bundle{
		ID: "sample",
		SelfTest: func() []core.SelfTestSection {
			return []core.SelfTestSection{{
				Name: "sample",
				Features: []core.SelfTestFeature{
					{Name: "Include a reusable pipeline"},
					{Name: "Another feature"},
					{Name: "Handle an edge case"},
				},
			}}
		},
	}

	sections := SectionsFromBundles([]core.Bundle{bundle})
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(sections))
	}
	got := sections[0].Features
	want := []string{
		"Include a reusable pipeline",
		"Another feature",
		"Handle an edge case",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d features, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("feature %d name = %q, want %q", i, got[i].Name, want[i])
		}
	}
}
