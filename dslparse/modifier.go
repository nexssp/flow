package dslparse

// ModifierValue to neutralna wartość wyemitowana przez parser Flow.
// Parser nie wie, czym jest HTTP, NATS, cron ani żaden protokół.
type Modifier struct {
	Kind string
	Raw  string
}
