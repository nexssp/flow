package core

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// Expr is the root of the AST. Every node knows how to report its children,
// how to validate its contract (Analyze), and how to lower itself into a
// runnable action (Build). Core never switches on concrete AST types.
type Expr interface {
	Node()
	Children() []Expr
	Analyze(resolver CapabilityResolver) error
	Build(ctx context.Context, bCtx *BuildContext) (action.AnyAction, error)
}

// Atom is a single runtime action call.
type Atom struct {
	Name            string
	Params          map[string]string
	Modifiers       []string
	ModifierSources []ModifierSource // parallel to Modifiers; same length
	Args            map[string]*Value
	Prompt          string
	Targets         []string
	Excludes        []string
	// ConfigInject marks an atom written as `op @config`. For stream
	// operators, every field of the operator's config struct that has
	// a matching @config.<json_name> value is injected into its
	// parameters at build time.
	ConfigInject bool
	Pos          Position // source position of the atom, for compile-time diagnostics
}

// ModifierSource records where a modifier came from. Both fields are
// free-form strings: core never interprets them. Extensions and the
// parser populate them; `nflow explain` renders them.
//
// Kind is a short category. Reserved values: "atom" for a modifier
// written directly on the atom. Extensions add their own
// ("scope", "profile", "pipeline", …).
//
// Label identifies the specific origin within a kind — a profile name,
// a pipeline name. Empty when the kind alone is enough.
type ModifierSource struct {
	Kind  string
	Label string
}

func (*Atom) Node()            {}
func (*Atom) Children() []Expr { return nil }

// PipeExpr: left -> right.
type PipeExpr struct{ L, R Expr }

func (*PipeExpr) Node()              {}
func (p *PipeExpr) Children() []Expr { return []Expr{p.L, p.R} }

// ParallelExpr: N branches executed concurrently.
type ParallelExpr struct{ Branches []Expr }

func (p *ParallelExpr) Children() []Expr { return p.Branches }
func (*ParallelExpr) Node()              {}

// FallbackExpr: left || right.
type FallbackExpr struct{ L, R Expr }

func (*FallbackExpr) Node()              {}
func (f *FallbackExpr) Children() []Expr { return []Expr{f.L, f.R} }

// ConditionalExpr: cond ? then : else.
type ConditionalExpr struct {
	Cond Expr
	Then Expr
	Else Expr
}

func (*ConditionalExpr) Node() {}
func (c *ConditionalExpr) Children() []Expr {
	if c.Else == nil {
		return []Expr{c.Cond, c.Then}
	}
	return []Expr{c.Cond, c.Then, c.Else}
}

// ProjectionExpr: raw content `{ ... }`.
type ProjectionExpr struct{ Raw string }

func (*ProjectionExpr) Node()              {}
func (p *ProjectionExpr) Children() []Expr { return nil }

// LoopExpr: loop(body) until(condition).
type LoopExpr struct {
	Body  Expr
	Until string
}

func (*LoopExpr) Node()              {}
func (l *LoopExpr) Children() []Expr { return []Expr{l.Body} }

// AssertExpr: assert(condition, message).
type AssertExpr struct {
	Condition string
	Message   string
}

func (*AssertExpr) Node()              {}
func (a *AssertExpr) Children() []Expr { return nil }

type ValueKind uint8

const (
	ValueString ValueKind = iota
	ValueBare
	ValueNumber
	ValueBool
	ValueNull
	ValueRef
	ValueMap
	ValueSlice
)

type Value struct {
	Kind  ValueKind
	Str   string
	Num   float64
	Bool  bool
	Ref   string
	Map   []MapEntry
	Slice []*Value
}

type MapEntry struct {
	Key   string
	Value *Value
}

func (v *Value) IsWholeStateRef() bool {
	return v != nil && v.Kind == ValueRef && v.Ref == ""
}

func AtomNames(e Expr) []string {
	var out []string
	walkExpr(e, func(node Expr) {
		switch n := node.(type) {
		case *Atom:
			out = append(out, n.Name)
		case *ProjectionExpr:
			out = append(out, "{ projection }")
		}
	})
	return out
}

func walkExpr(e Expr, visit func(Expr)) {
	if e == nil {
		return
	}
	visit(e)
	for _, child := range e.Children() {
		walkExpr(child, visit)
	}
}
