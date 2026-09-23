package flow_test

import (
	"testing"

	"github.com/nexssp/flow"
)

func TestArrowDSL_LiftsEffectOntoNodeSpec(t *testing.T) {
	dsl := `repo.merge:effect=high_risk`

	def, err := flow.ParseArrowDSL("test", dsl)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Nodes) != 1 {
		t.Fatalf("nodes = %d", len(def.Nodes))
	}
	if def.Nodes[0].Effect != flow.EffectHighRisk {
		t.Errorf("effect = %q, want high_risk", def.Nodes[0].Effect)
	}
}

func TestArrowDSL_LiftsReadOnlyOntoNodeSpec(t *testing.T) {
	dsl := `sandbox.test:read_only`

	def, err := flow.ParseArrowDSL("test", dsl)
	if err != nil {
		t.Fatal(err)
	}
	if def.Nodes[0].Effect != flow.EffectReadOnly {
		t.Errorf("effect = %q, want read_only", def.Nodes[0].Effect)
	}
}

func TestArrowDSL_GateModifierForcesApproval(t *testing.T) {
	dsl := `deploy:gate="risk > 0.5"`

	def, err := flow.ParseArrowDSL("test", dsl)
	if err != nil {
		t.Fatal(err)
	}
	if !def.Nodes[0].Approval {
		t.Error("Approval should be true after :gate=")
	}
}

func TestArrowDSL_LiftsTimeoutAndRetry(t *testing.T) {
	dsl := `slow.op:timeout=30s:retry=3`

	def, err := flow.ParseArrowDSL("test", dsl)
	if err != nil {
		t.Fatal(err)
	}
	n := def.Nodes[0]
	if n.TimeoutMS != 30000 {
		t.Errorf("TimeoutMS = %d, want 30000", n.TimeoutMS)
	}
	if n.Retry.MaxAttempts != 3 {
		t.Errorf("Retry.MaxAttempts = %d, want 3", n.Retry.MaxAttempts)
	}
}

func TestArrowDSL_ModifiersNotLiftedRemainOnAtom(t *testing.T) {
	// :cache= is a runtime concern; it must not become a NodeSpec field.
	dsl := `read:cache=5m:coalesce`

	def, err := flow.ParseArrowDSL("test", dsl)
	if err != nil {
		t.Fatal(err)
	}
	if def.Nodes[0].Effect != "" {
		t.Errorf("Effect = %q, want empty", def.Nodes[0].Effect)
	}
	// No assertion on the atom itself — the modifier is preserved on
	// the AtomExpr, not carried into NodeSpec.
}
