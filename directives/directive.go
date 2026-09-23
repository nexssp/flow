// Package directives holds the DSL directive registry and the shipped
// directives themselves.
//
// The shared types and helpers live in the `core` subpackage so that
// future directives can be moved into their own folders without
// creating an import cycle. This file re-exports everything existing
// directive implementations refer to, so no caller changes when a
// directive moves.
package directives

import (
	"fmt"

	// Activate the shipped directives. Any directive moved into
	// directives/builtin/<name>/ is picked up here.
	_ "github.com/nexssp/flow/directives/builtin"

	"github.com/nexssp/flow/directives/core"
)

// Type aliases. Code in this package keeps using the	 bare names.
type (
	Position           = core.Position
	Context            = core.Context
	Preprocessed       = core.Preprocessed
	AssertDecl         = core.AssertDecl
	GateRule           = core.GateRule
	Pool               = core.Pool
	CapabilityEnvelope = core.CapabilityEnvelope
	Pipeline           = core.Pipeline
	ActionMeta         = core.ActionMeta
	Requirement        = core.Requirement
	Directive          = core.Directive

	OnErrorDecl = core.OnErrorDecl
	OnErrorRule = core.OnErrorRule
	// RetryDecl   = core.RetryDecl
)

// Function re-exports.
func Register(d Directive) { core.Register(d) }

func Lookup(line string) (Directive, bool) { return core.Lookup(line) }

func All() []Directive { return core.All() }

func Names() []string { return core.Names() }

func StripDirectivePrefix(line, name string) (string, bool) {
	return core.StripDirectivePrefix(line, name)
}

func SplitNameAndAttrs(s string) (string, map[string]string, error) {
	return core.SplitNameAndAttrs(s)
}

func ParseList(s string) []string { return core.ParseList(s) }

func TrimQuotes(s string) string { return core.TrimQuotes(s) }

func ParseFloat(s string) (float64, error) { return core.ParseFloat(s) }

func ParseInt(s string) (int, error) { return core.ParseInt(s) }

func AtErr(ctx *Context, line int, name, msg string) error {
	return core.AtErr(ctx, line, name, msg)
}

func AtErrf(ctx *Context, line int, name, format string, args ...any) error {
	return core.AtErrf(ctx, line, name, format, args...)
}

func SplitBlock(lines []string, start int) (header string, body []string, next int, err error) {
	return core.SplitBlock(lines, start)
}

// errf stays here because at_profile.go uses it during merge.
func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
