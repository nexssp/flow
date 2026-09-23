// file: flow/directives/builtin/at_on/on_test.go
package at_on_test

import (
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/directives/builtin/at_on"
)

func TestAtOn_Parse(t *testing.T) {
	t.Parallel()

	input := `@on event "nats:jobs.inbox"

jobs.process -> jobs.done
`

	pre, err := flow.PreprocessBytes([]byte(input), "service.nflow")
	if err != nil {
		t.Fatalf("PreprocessBytes failed: %v", err)
	}

	raw, ok := pre.Declarations[at_on.DeclarationKey]
	if !ok {
		t.Fatalf("expected declaration key %q", at_on.DeclarationKey)
	}

	trigger, ok := raw.(at_on.EventTrigger)
	if !ok {
		t.Fatalf("expected type EventTrigger, got %T", raw)
	}

	if trigger.Protocol != "nats" {
		t.Errorf("expected Protocol 'nats', got %q", trigger.Protocol)
	}
	if trigger.Target != "jobs.inbox" {
		t.Errorf("expected Target 'jobs.inbox', got %q", trigger.Target)
	}
}
