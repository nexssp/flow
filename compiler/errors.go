package compiler

import (
	"fmt"

	"github.com/nexssp/kernel/xerr"
)

// Position identifies a source location inside a .nflow file. It is the
// lowest-level error-position type in the flow stack; higher layers
// (flow, runner) re-export it so callers do not need to import
// compiler directly.
type Position struct {
	File string
	Line int
	Col  int
}

// String renders the position as "path/to/file.nflow:12:5". An empty
// File is rendered as "<input>" so test fixtures and in-memory DSL
// strings still produce a usable location.
func (p Position) String() string {
	file := p.File
	if file == "" {
		file = "<input>"
	}
	if p.Line <= 0 {
		return file
	}
	if p.Col <= 0 {
		return fmt.Sprintf("%s:%d", file, p.Line)
	}
	return fmt.Sprintf("%s:%d:%d", file, p.Line, p.Col)
}

// SourceError formats a parse- or compile-time error with a source
// position. Every message has the shape "file:line:col: msg", the
// same convention used by go/parser and gopls, so editors and human
// readers both get the familiar format.
//
// The error kind is BadRequest, which every transport in the Nexss
// ecosystem maps to HTTP 400 without further inspection.
func SourceError(pos Position, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)

	return xerr.BadRequest(fmt.Sprintf("%s: %s", pos.String(), msg))
}

// NeedsPipelineError is returned by ParseArrowDSL when the AST contains
// a construct the static DAG model cannot represent.
//
// The three constructs that trigger it are loop(), conditional (? :),
// and fallback (||). They are runtime compositions, not graph topology:
// a DAG is by definition acyclic and cannot encode an unbounded loop,
// a data-dependent branch, or a try-then-else chain. CompilePipeline
// handles all three by composing actions, so callers that can execute
// a composed action should detect this error and fall back.
//
// Callers should use errors.As to test for this type:
//
//	var npe *compiler.NeedsPipelineError
//	if errors.As(err, &npe) {
//	    // fall back to CompilePipeline
//	}
type NeedsPipelineError struct {
	Reason string
}

func (e *NeedsPipelineError) Error() string {
	if e == nil {
		return "graph: DSL requires the pipeline compiler"
	}
	if e.Reason == "" {
		return "graph: DSL requires the pipeline compiler, not the static DAG"
	}
	return "graph: " + e.Reason
}
