package compiler

import (
	"context"

	"github.com/nexssp/kernel/action"
)

type DirectiveReq struct {
	Lines   []string
	Body    []string
	I       int
	Out     map[string]any
	File    string
	BaseDir string
	Table   *DirectiveTable

	// embedSource is installed by the source-composition lifecycle.
	embedSource func(string, []SourceLine)

	// Recurse runs the full preprocess on another .nflow file and
	// returns its cleaned body and file-local compilation state.
	Recurse func(absPath string) (clean string, meta map[string]any, err error)

	// Import parses a module in its own source scope. It never merges Out.
	Import func(target, namespace string) error
}

type DirectiveRes struct {
	Next int

	// BlankLines lists 0-based line indices whose output must be empty
	// even when the source line has content. Block directives use it
	// to remove their own delimiter lines (e.g. @scope's header and
	// closing brace) without consuming the body, so nested directives
	// inside the body still run through the outer preprocess loop.
	BlankLines []int
}

type Directive struct {
	Name        string
	Example     string
	Handler     func(context.Context, DirectiveReq) (DirectiveRes, error)
	OpaqueBlock bool // The directive owns a `{ ... }` body that formatters must preserve verbatim.
}

func (d Directive) Action() *action.BuiltAction[DirectiveReq, DirectiveRes] {
	return action.New("directive."+d.Name, d.Handler).
		Description("@"+d.Name).
		Tag("directive", d.Name).
		Build()
}

type DirectiveTable struct {
	byName  map[string]*action.BuiltAction[DirectiveReq, DirectiveRes]
	ordered []Directive
}

func NewDirectiveTable(ds ...Directive) *DirectiveTable {
	t := &DirectiveTable{
		byName:  make(map[string]*action.BuiltAction[DirectiveReq, DirectiveRes], len(ds)),
		ordered: append([]Directive(nil), ds...),
	}
	for _, d := range ds {
		if d.Name == "" {
			panic("core: directive with empty name")
		}
		if _, dup := t.byName[d.Name]; dup {
			panic("core: duplicate directive " + d.Name)
		}
		t.byName[d.Name] = d.Action()
	}
	return t
}

func (t *DirectiveTable) ByName(name string) (*action.BuiltAction[DirectiveReq, DirectiveRes], bool) {
	if t == nil {
		return nil, false
	}
	a, ok := t.byName[name]
	return a, ok
}

func (t *DirectiveTable) Actions() []action.AnyAction {
	if t == nil {
		return nil
	}
	out := make([]action.AnyAction, 0, len(t.ordered))
	for _, d := range t.ordered {
		out = append(out, t.byName[d.Name])
	}
	return out
}

func (t *DirectiveTable) All() []Directive {
	if t == nil {
		return nil
	}
	return append([]Directive(nil), t.ordered...)
}
