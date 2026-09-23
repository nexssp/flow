package directives_test

import (
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/directives"
)

func TestAtConfig_BlockSyntax(t *testing.T) {
	t.Parallel()

	src := `@config {
  targets:  ["."]     :cli="target,t" :desc="Target directory"
  ext:      "go,ts"   :cli="ext"      :desc="Extensions"
  sig:      true      :cli="sig"      :desc="Signatures only"
  command:  "code"    :positional=0
}

noop
`

	pre, err := flow.PreprocessBytes([]byte(src), "config_test.nflow")
	if err != nil {
		t.Fatalf("PreprocessBytes failed: %v", err)
	}

	// 1. Płaska mapa wartości domyślnych
	if pre.Config["targets"] != `["."]` {
		t.Errorf("targets = %q, want [.]", pre.Config["targets"])
	}
	if pre.Config["ext"] != "go,ts" {
		t.Errorf("ext = %q, want go,ts", pre.Config["ext"])
	}
	if pre.Config["sig"] != "true" {
		t.Errorf("sig = %q, want true", pre.Config["sig"])
	}
	if pre.Config["command"] != "code" {
		t.Errorf("command = %q, want code", pre.Config["command"])
	}

	// 2. Metadane pól dla CLI
	raw, ok := pre.Declarations["config_fields"]
	if !ok {
		t.Fatal("missing Declarations[config_fields]")
	}
	specs := raw.([]directives.ConfigFieldSpec)
	if len(specs) != 4 {
		t.Fatalf("expected 4 config field specs, got %d", len(specs))
	}

	if specs[0].Key != "targets" || specs[0].CLI != "target,t" || specs[0].Desc != "Target directory" {
		t.Errorf("specs[0] mismatch: %+v", specs[0])
	}
	if specs[2].Key != "sig" || specs[2].CLI != "sig" {
		t.Errorf("specs[2] mismatch: %+v", specs[2])
	}
}

func TestAtConfig_SingleLineStillWorks(t *testing.T) {
	t.Parallel()

	src := `@config:budget_usd=0.50
@config:approval="danger"

noop
`
	pre, err := flow.PreprocessBytes([]byte(src), "config_single.nflow")
	if err != nil {
		t.Fatalf("PreprocessBytes failed: %v", err)
	}

	if pre.Config["budget_usd"] != "0.50" {
		t.Errorf("budget_usd = %q", pre.Config["budget_usd"])
	}
	if pre.Config["approval"] != "danger" {
		t.Errorf("approval = %q", pre.Config["approval"])
	}
}
