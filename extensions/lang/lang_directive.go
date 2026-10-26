// Package lang — file-scope directives for runner definitions.
//
// @lang.runner and @lang.define are compiler.Directive extensions, like
// @include and @assert. They run at preprocess time (line-based), before
// the parser sees the expression grammar.
//
// @lang.runner is the file-scope form of what runners/*.nflow ships as
// data: it lets a flow declare a language inline. @lang.define is the
// runtime-only form (no install recipe) — the data model is shared.
package lang

import (
	"context"
	"strings"

	"github.com/nexssp/flow/compiler"
)

// RunnerDirective is the @lang.runner directive. It parses the runner
// spec from the directive body and registers it via registerSpec (the
// same path LoadRunners uses for embedded runner files).
//
// Syntax (file-scope, one per line until the closing }):
//
//	@lang.runner "python" {
//	    "id": "python",
//	    "ext": ".py",
//	    "aliases": ["py", "python"],
//	    "find": ["python3", "python"],
//	    "stdin": "json",
//	    "stdout": "json",
//	    "install": {
//	        "windows": ["winget", "install", "Python.Python.3.12"],
//	        "darwin": ["brew", "install", "python@3.12"]
//	    }
//	}
//
// The body is strict JSON, as in embedded runners/*.nflow files. Use an
// optional `_comment` field for notes. The same ParseRunner handles both.
var RunnerDirective = compiler.Directive{
	Name:    "lang.runner",
	Example: `@lang.runner "python" { "id": "python", "ext": ".py", "find": ["python3"] }`,
	Handler: handleRunnerDirective,
}

// DefineDirective is the @lang.define directive. Same data model, but
// intended for runtime-only languages (no install recipe). The body
// format is identical to @lang.runner.
var DefineDirective = compiler.Directive{
	Name:    "lang.define",
	Example: `@lang.define "gleam" { "id": "gleam", "ext": ".gleam", "find": ["gleam", "run"] }`,
	Handler: handleDefineDirective,
}

// handleRunnerDirective parses the @lang.runner body and registers the
// language via registerSpec (the shared path with LoadRunners).
//
// The directive receives lines via req.Lines; we reconstruct the full
// @lang.runner header + body and delegate to ParseRunner, which is the
// single parser for this format. This avoids a second parser.
func handleRunnerDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	raw, next, err := collectDirectiveBody(req, pos)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	spec, err := ParseRunner(raw)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@lang.runner: %w", err)
	}
	if err := registerSpec(spec); err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@lang.runner: %w", err)
	}
	return compiler.DirectiveRes{Next: next}, nil
}

// handleDefineDirective is the same as handleRunnerDirective but routes
// through RegisterDefine so the error messages say @lang.define.
// Internally RegisterDefine calls registerSpec — same code path.
func handleDefineDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	raw, next, err := collectDirectiveBody(req, pos)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	spec, err := ParseRunner(raw)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@lang.define: %w", err)
	}
	// Convert the parsed RunnerSpec to a Define and register. The two
	// structs have the same fields; Define is the user-facing name,
	// RunnerSpec is the internal name. registerSpec handles both.
	d := Define{
		ID:      spec.ID,
		Version: spec.Version,
		Ext:     spec.Ext,
		Aliases: spec.Aliases,
		Find:    spec.Find,
		Stdin:   spec.Stdin,
		Stdout:  spec.Stdout,
		Install: spec.Install,
	}
	if err := RegisterDefine(d); err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@lang.define: %w", err)
	}
	return compiler.DirectiveRes{Next: next}, nil
}

// collectDirectiveBody reconstructs the @lang.runner / @lang.define
// header + body from the directive request's line slice, so we can
// delegate to ParseRunner (the single parser for this format).
//
// The directive request gives us req.Lines starting at req.I. We scan
// forward from req.I for the opening { and then the matching close },
// depth-aware. We return the reconstructed source and the index of the
// line AFTER the closing brace (so the compiler knows where to resume).
//
// This mirrors the line-scanning that @include does, but for a
// multi-line brace-delimited body rather than a single-line path.
func collectDirectiveBody(req compiler.DirectiveReq, pos compiler.Position) (body string, next int, err error) {
	if req.I >= len(req.Lines) {
		return "", 0, compiler.SourceError(pos, "@lang.runner: no body")
	}
	// Join lines from req.I onward; ParseRunner handles the @lang.runner
	// header and the { ... } body.
	var b strings.Builder
	depth := 0
	started := false
	for j := req.I; j < len(req.Lines); j++ {
		line := req.Lines[j]
		b.WriteString(line)
		b.WriteByte('\n')
		for _, r := range line {
			switch r {
			case '{':
				depth++
				started = true
			case '}':
				depth--
			}
		}
		if started && depth == 0 {
			return b.String(), j + 1, nil
		}
	}
	return "", 0, compiler.SourceError(pos, "@lang.runner: unterminated body (missing '}')")
}
