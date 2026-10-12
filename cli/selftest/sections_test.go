package selftest

import (
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestSectionsFromBundlesPreservesFeatureDescriptions(t *testing.T) {
	bundle := compiler.Bundle{
		ID: "sample",
		SelfTest: func() []compiler.SelfTestSection {
			return []compiler.SelfTestSection{{
				Name: "sample",
				Features: []compiler.SelfTestFeature{
					{Name: "Include a reusable pipeline"},
					{Name: "Another feature"},
					{Name: "Handle an edge case"},
				},
			}}
		},
	}

	sections := SectionsFromBundles([]compiler.Bundle{bundle})
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
