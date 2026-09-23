// file: flow/directives/at_require_test.go
package directives_test

import (
	"testing"

	"github.com/nexssp/flow"
)

func TestRequire_OptionsBlock(t *testing.T) {
	t.Parallel()

	nflowContent := `
@require github.com/nexssp/transportnats v1.0.1 {
  timeout: "5s",
  retries: "3"
}

@require "github.com/nexssp/transportai@v1.0.0" { mode: "strict" }

noop -> noop
`

	pre, err := flow.PreprocessBytes([]byte(nflowContent), "test.nflow")
	if err != nil {
		t.Fatalf("PreprocessBytes failed: %v", err)
	}

	if len(pre.Requires) != 2 {
		t.Fatalf("expected 2 requirements, got %d", len(pre.Requires))
	}

	natsReq := pre.Requires[0]
	if natsReq.Import != "github.com/nexssp/transportnats" || natsReq.Version != "v1.0.1" {
		t.Fatalf("unexpected nats module: %+v", natsReq)
	}
	if natsReq.Options["timeout"] != "5s" || natsReq.Options["retries"] != "3" {
		t.Fatalf("expected nats options {timeout: 5s, retries: 3}, got: %+v", natsReq.Options)
	}

	aiReq := pre.Requires[1]
	if aiReq.Import != "github.com/nexssp/transportai" || aiReq.Version != "v1.0.0" {
		t.Fatalf("unexpected ai module: %+v", aiReq)
	}
	if aiReq.Options["mode"] != "strict" {
		t.Fatalf("expected ai options {mode: strict}, got: %+v", aiReq.Options)
	}
}
