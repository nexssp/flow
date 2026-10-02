// Package render provides the render.markdown stream operator, which
// reformats FileContent into a single Markdown document. The editor
// option selects the framing (markdown, zed, claude, fold).
//
// Typical use:
//
//	fs.walk -> fs.filter:ext="go" -> fs.read -> render.markdown:editor="zed" -> out.file:path="context.md"
package render

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "render"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		SelfTest:  selftest,
	}
}

func Library() action.Library {
	return action.Library{
		Name:      ID,
		Operators: []action.NamedOperator{MarkdownOperator()},
	}
}
