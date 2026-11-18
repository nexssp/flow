// Package template provides a reusable Go text-template renderer and exposes
// it to Flow as the template.render action.
//
// Typical use:
//
//	template.render(template: "Hello {{.name}}", variables: { name: "Flow" })
//
// An embedded template is opened through the host's configured file resolver:
//
//	@embed welcome "templates/welcome.tmpl"
//	template.render(template: embed.welcome, variables: { name: "Flow" })
//
// To load runtime template files, pass each fs.read Content byte slice as the
// source. Since fs.read is a stream, collect it and call a small pipeline per
// file:
//
//	@pipeline render_template_file
//	  template.render(template_bytes: .Content, variables: { name: "Flow" })
//	@end
//	fs.walk(dir: "templates", ext: "tmpl") -> fs.read -> collect ->
//	  { files: . } -> distribute.map(action: pipeline.render_template_file, items: .files)
package template

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "template"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle exposes the template.render action to Flow.
func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

// Library returns the action library without requiring registry lookup.
func Library() action.Library {
	return action.Library{
		Name:    ID,
		Actions: []action.AnyAction{RenderAction},
	}
}
