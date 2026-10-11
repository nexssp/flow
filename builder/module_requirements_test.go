package builder

import "testing"

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
