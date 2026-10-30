// Package test provides an opt-in @test directive for nflow test runs.
//
// The bundle is intentionally not registered in the process-wide compiler
// registry or added to native.Bundles. Hosts mount it only when test behavior
// is explicitly enabled.
package test

import (
	"context"
	"errors"
	"strings"

	"github.com/nexssp/flow/compiler"
	assert "github.com/nexssp/flow/extensions/assert"
)

const ID = "test"

// Directive records a post-run assertion using the same syntax and metadata as
// @assert. Its availability is controlled by the host that mounts Bundle.
var Directive = compiler.Directive{
	Name:        "test",
	Example:     "@test: result.ok == true\n@test {\n  result.count > 0\n}",
	Handler:     handleDirective,
	OpaqueBlock: true,
}

// Bundle returns the explicit, test-mode-only directive bundle. It is not
// registered automatically so normal nflow run invocations cannot see @test.
func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:         ID,
		Directives: []compiler.Directive{Directive},
	}
}

func handleDirective(ctx context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	if req.I < 0 || req.I >= len(req.Lines) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@test: directive line is unavailable")
	}

	line := req.Lines[req.I]
	before, after, found := strings.Cut(line, "@test")
	if !found {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@test: directive name is missing")
	}

	lines := append([]string(nil), req.Lines...)
	lines[req.I] = before + "@assert" + after
	delegated := req
	delegated.Lines = lines

	result, err := assert.Directive.Handler(ctx, delegated)
	if err != nil {
		return compiler.DirectiveRes{}, rewriteDirectiveError(err)
	}
	return result, nil
}

func rewriteDirectiveError(err error) error {
	message := strings.ReplaceAll(err.Error(), "@assert", "@test")
	if pos, ok := compiler.PositionOf(err); ok {
		message = strings.TrimPrefix(message, pos.String()+": ")
		return compiler.SourceError(pos, "%s", message)
	}
	return errors.New(message)
}
