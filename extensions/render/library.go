// Package render provides the render.markdown stream operator, which
// reformats FileContent into a single Markdown document. The editor
// option selects the framing (markdown, zed, claude, fold).
//
// Typical use:
//
//	fs.walk -> fs.filter:ext="go" -> fs.read -> render.markdown:editor="zed" -> out.file:path="context.md"
package render

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "render"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name:      ID,
		Operators: []action.NamedOperator{MarkdownOperator()},
	}
}
