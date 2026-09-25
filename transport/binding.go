package transport

import (
	"github.com/nexssp/flow/dslparse"
	"github.com/nexssp/kernel/action"
)

// Modifier alias to dslparse.Modifier
type Modifier = dslparse.Modifier

// DSLBinding oznacza akcję odpowiedzialną za rezolucję konkretnego modyfikatora DSL.
type DSLBinding struct{ Kind string }

// TriggerBinding oznacza akcję będącą punktem wejścia dla danego protokołu.
type TriggerBinding struct{ Protocol string }

func OnDSL(kind string) action.Binding {
	if kind == "" {
		panic("flow/transport: empty modifier name OnDSL")
	}
	return DSLBinding{Kind: kind}
}

func OnTrigger(protocol string) action.Binding {
	if protocol == "" {
		panic("flow/transport: empty protocol OnTrigger")
	}
	return TriggerBinding{Protocol: protocol}
}
