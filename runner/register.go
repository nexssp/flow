package runner

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
)

// Registrar is the entry point every @require'd library must export.
//
// The generated requires_gen.go calls this once per @require with the
// options declared in the .nflow file. The library returns a fully
// formed action.Library; the caller assembles the final registry.
//
// The signature takes no *action.Registry because registries are
// immutable. A library cannot add actions to an existing registry; it
// returns its own library and lets the caller compose.
type Registrar func(opts map[string]string, sink observe.Sink) (action.Library, error)

// ValidateOptions rejects unknown keys against a fixed allowlist.
// Every library should call this at the top of its Register to fail
// fast on typos or outdated options.
func ValidateOptions(opts map[string]string, allowed ...string) error {
	if len(opts) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allow[k] = true
	}
	for k := range opts {
		if !allow[k] {
			return fmt.Errorf(
				"unknown option %q (allowed: %s)",
				k, strings.Join(allowed, ", "))
		}
	}
	return nil
}
