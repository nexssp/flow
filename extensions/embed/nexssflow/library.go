// Package nexssflow exposes the embed extension through Flow's external-bundle adapter convention.
package nexssflow

import (
	"github.com/nexssp/flow/compiler"
	embedext "github.com/nexssp/flow/extensions/embed"
)

// Bundle returns the embed bundle configured by an @require declaration.
func Bundle(opts map[string]string) compiler.Bundle {
	return embedext.Bundle(opts)
}
