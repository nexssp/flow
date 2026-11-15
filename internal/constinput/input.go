// Package constinput defines compiler/runtime values used to preserve the
// distinction between explicit scalar input and a named const argument.
package constinput

// Direct carries a scalar written directly after the const keyword.
type Direct struct {
	Value any
}

// String marks a compile-time string value that const must not auto-coerce.
type String string
