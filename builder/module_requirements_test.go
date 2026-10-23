package builder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/flow/extensions/require"
)

func TestRequirementsToRetainUsesDriverVersionWhenHarnessIsStale(t *testing.T) {
	driver := []moduleRequirement{{path: "github.com/nexssp/kernel", version: "v0.30.0"}}
	harness := []moduleRequirement{{path: "github.com/nexssp/kernel", version: "v0.28.0"}}

	got := requirementsToRetain(driver, harness)
	if len(got) != 1 {
		t.Fatalf("retained %d requirements, want 1", len(got))
	}
	if got[0].path != "github.com/nexssp/kernel" || got[0].version != "v0.30.0" {
		t.Fatalf("retained requirement = %#v, want github.com/nexssp/kernel@v0.30.0", got[0])
	}
}

func TestRequirementsToRetainSkipsMatchingAndFlowRoot(t *testing.T) {
	driver := []moduleRequirement{
		{path: "github.com/nexssp/flow", version: "v1.2.3"},
		{path: "github.com/nexssp/kernel", version: "v0.30.0"},
	}
	harness := []moduleRequirement{{path: "github.com/nexssp/kernel", version: "v0.30.0"}}

	if got := requirementsToRetain(driver, harness); len(got) != 0 {
		t.Fatalf("retained requirements = %#v, want none", got)
	}
}

func TestRequirementsToRetainPreservesHigherSelectedVersion(t *testing.T) {
	driver := []moduleRequirement{{path: "github.com/nexssp/kernel", version: "v0.30.0"}}
	harness := []moduleRequirement{{path: "github.com/nexssp/kernel", version: "v0.35.0"}}

	if got := requirementsToRetain(driver, harness); len(got) != 0 {
		t.Fatalf("higher selected requirement was downgraded/retained unnecessarily: %#v", got)
	}
}

func TestWriteRequiresSelectsHighestProviderVersion(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		requirements := []require.Requirement{
			{Import: "example.org/shared/a", ModulePath: "example.org/shared", ResolvedVersion: "v1.0.0"},
			{Import: "example.org/shared/b", ModulePath: "example.org/shared", ResolvedVersion: "v1.3.0"},
		}
		if reversed {
			requirements[0], requirements[1] = requirements[1], requirements[0]
		}
		var output strings.Builder
		writeRequires(&output, requirements, "v0.36.0", "")
		if strings.Count(output.String(), "example.org/shared ") != 1 || !strings.Contains(output.String(), "example.org/shared v1.3.0") {
			t.Fatalf("MVS provider requirements = %s", output.String())
		}
	}
}

func TestGoCommandFailureRetainsContextClassification(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		want := context.Canceled
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(t.Context(), time.Time{})
			want = context.DeadlineExceeded
		}
		cancel()
		err := goCommandFailure(ctx, []string{"list"}, errors.New("exit status 1"), "diagnostic")
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
	}
}
