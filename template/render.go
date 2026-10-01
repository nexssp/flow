// Package template provides package-agnostic text template rendering.
//
// It uses Go's standard text/template syntax and never executes rendered text
// as a command. The renderer returns plain text; it does not HTML-escape output.
package template

import (
	"bytes"
	"errors"
	"fmt"
	texttemplate "text/template"
)

var (
	// ErrParse identifies invalid template syntax or an unknown template function.
	ErrParse = errors.New("template parse error")
	// ErrExecute identifies a failure while evaluating a parsed template,
	// including a reference to a missing map key.
	ErrExecute = errors.New("template execution error")
)

// Render evaluates source with variables as the template's root data.
//
// Templates use Go text/template syntax, for example `Hello {{.name}}`.
// A missing map key is an error (rather than silently rendering an empty value).
// Extra keys in variables are allowed and ignored when unused. A nil variables
// map behaves like an empty map. On any error, Render returns an empty string,
// never partial output.
//
// Values are inserted as text and are not parsed again as template source.
// Render does not execute shell commands and does not HTML-escape its result.
func Render(source string, variables map[string]any) (string, error) {
	tmpl, err := texttemplate.New("render").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrParse, err)
	}

	var output bytes.Buffer
	if err := tmpl.Execute(&output, variables); err != nil {
		return "", fmt.Errorf("%w: %w", ErrExecute, err)
	}
	return output.String(), nil
}
