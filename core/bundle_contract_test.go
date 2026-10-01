package core

import (
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestValidateBundleRequiresIdentityAndLibraries(t *testing.T) {
	t.Parallel()

	if err := ValidateBundle(Bundle{}); err == nil {
		t.Fatal("expected missing bundle ID error")
	}
	if err := ValidateBundle(Bundle{ID: "example"}); err != nil {
		t.Fatalf("syntax-only bundle should be valid: %v", err)
	}
	if err := ValidateBundle(Bundle{ID: "example", Libraries: []action.Library{{Name: "x"}, {Name: "x"}}}); err == nil {
		t.Fatal("expected duplicate library error")
	}
}

func TestValidateBundleAllowsMultipleLibraries(t *testing.T) {
	t.Parallel()

	err := ValidateBundle(Bundle{
		ID: "nexss.ai",
		Libraries: []action.Library{
			{Name: "ai.prompt"},
			{Name: "ai.agent"},
		},
	})
	if err != nil {
		t.Fatalf("multiple libraries should be valid: %v", err)
	}
}
