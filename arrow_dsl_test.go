package flow_test

import (
	"testing"

	"github.com/nexssp/flow"
)

func TestParseArrowDSL_ComplexTopology(t *testing.T) {
	t.Parallel()

	dsl := "srcpack.pack:arch#internal/auth~testdata@security -> (sec.audit & arch.review & perf.bench) -> review.gate"
	def, err := flow.ParseArrowDSL("security_pipeline", dsl)
	if err != nil {
		t.Fatalf("ParseArrowDSL failed: %v", err)
	}

	if def.Metadata.Name != "security_pipeline" {
		t.Errorf("expected graph name 'security_pipeline', got %q", def.Metadata.Name)
	}

	// 1 root (pack) + 3 parallel (audit, review, bench) + 1 gate = 5 nodes
	if len(def.Nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(def.Nodes))
	}

	// Edges: 1 -> 3 (fan-out: 3 edges) + 3 -> 1 (fan-in: 3 edges) = 6 edges
	if len(def.Edges) != 6 {
		t.Fatalf("expected 6 edges, got %d", len(def.Edges))
	}

	// Verify parameter extraction on the first stage node
	firstNode := def.Nodes[0]
	if firstNode.Capability != "srcpack.pack" {
		t.Errorf("expected capability 'srcpack.pack', got %q", firstNode.Capability)
	}
	if firstNode.Params["profile"] != "arch" {
		t.Errorf("expected profile 'arch', got %v", firstNode.Params["profile"])
	}
	if targets, ok := firstNode.Params["targets"].([]string); !ok || len(targets) != 1 || targets[0] != "internal/auth" {
		t.Errorf("expected targets ['internal/auth'], got %v", firstNode.Params["targets"])
	}
	if excludes, ok := firstNode.Params["excludes"].([]string); !ok || len(excludes) != 1 || excludes[0] != "testdata" {
		t.Errorf("expected excludes ['testdata'], got %v", firstNode.Params["excludes"])
	}
	if firstNode.Params["prompt"] != "security" {
		t.Errorf("expected prompt 'security', got %v", firstNode.Params["prompt"])
	}
}

func TestParseArrowDSL_InvalidSyntax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dsl  string
	}{
		{"Empty DSL", ""},
		{"Empty Stage", "pack -> -> gate"},
		{"Trailing Arrow", "pack -> "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := flow.ParseArrowDSL("invalid", tt.dsl)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.dsl)
			}
		})
	}
}

func TestParseArrowDSL_ExplicitFieldBindings(t *testing.T) {
	t.Parallel()

	dsl := "srcpack.pack:arch#pkg -> security.audit(code=srcpack.pack.content, env=staging) -> review.gate"
	def, err := flow.ParseArrowDSL("binding_pipeline", dsl)
	if err != nil {
		t.Fatalf("ParseArrowDSL failed: %v", err)
	}

	if len(def.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(def.Nodes))
	}

	secNode := def.Nodes[1]
	if secNode.Capability != "security.audit" {
		t.Errorf("expected capability 'security.audit', got %q", secNode.Capability)
	}

	// Verify upstream path routed to InputBindings
	if binding, ok := secNode.InputBindings["code"]; !ok || binding != "srcpack.pack.content" {
		t.Errorf("expected InputBindings['code'] = 'srcpack.pack.content', got %q", binding)
	}

	// Verify literal routed to Params
	if val, ok := secNode.Params["env"]; !ok || val != "staging" {
		t.Errorf("expected Params['env'] = 'staging', got %v", val)
	}
}
