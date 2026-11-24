package compiler

import (
	"context"
	"reflect"

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

type LiteralExpr struct {
	Val any
	Pos Position
}

func (*LiteralExpr) Node()                            {}
func (*LiteralExpr) Children() []Expr                 { return nil }
func (*LiteralExpr) Analyze(CapabilityResolver) error { return nil }
func (l *LiteralExpr) Build(_ context.Context, bCtx *BuildContext) (action.AnyAction, error) {
	act := action.New("literal", func(_ context.Context, _ any) (any, error) {
		return l.Val, nil
	}).Build()
	return inspectBuiltAction(bCtx, act), nil
}

// ValueExpr evaluates a parsed value against the current pipeline input.
// Unlike a LiteralExpr, it can contain references, collections, and typed
// symbol values that need runtime resolution.
type ValueExpr struct {
	Value *Value
	Pos   Position
}

func (*ValueExpr) Node()                            {}
func (*ValueExpr) Children() []Expr                 { return nil }
func (*ValueExpr) Analyze(CapabilityResolver) error { return nil }

// Atom is a single runtime action call.
type Atom struct {
	Name                  string
	Params                map[string]string
	ParamValues           map[string]any // native symbol values; takes precedence over Params
	Modifiers             []string
	ModifierValues        []ModifierValue  // parallel to Modifiers for quoted or native values
	ModifierSources       []ModifierSource // parallel to Modifiers; same length
	DirectInput           *Value           // literal or reference written as a pipeline value
	Args                  map[string]*Value
	ArgSchema             []ArgFieldSpec // copied capability argument types used by runtime guards
	SecretBoundary        bool
	SecretArgBoundary     bool
	SecretRequestType     reflect.Type
	SecretResponseType    reflect.Type
	SecretInputNormalizer bool
	ArgSpreads            []*Value // runtime maps spread into structured arguments
	HasArgumentObject     bool     // true when the atom used parenthesized named arguments
	// NamedPipelineArgs marks a named pipeline argument object as the complete
	// request. The pipeline extension sets this; values resolve against state at runtime.
	NamedPipelineArgs bool
	Prompt            string
	Targets           []string
	Excludes          []string
	// ConfigInject marks an atom written as `op @config`. For stream
	// sources and operators, fields matching @config.<json_name> values
	// are injected into their parameters at build time.
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
type PipeExpr struct {
	L, R Expr
	Pos  Position
}

func (*PipeExpr) Node()              {}
func (p *PipeExpr) Children() []Expr { return []Expr{p.L, p.R} }

// BindingExpr names the output of one pipeline step without changing the
// value passed to the next step. The syntax extension parses `step as name`.
type BindingExpr struct {
	Inner Expr
	Name  string
	Pos   Position
}

func (*BindingExpr) Node()              {}
func (b *BindingExpr) Children() []Expr { return []Expr{b.Inner} }

// ParallelExpr: N branches executed concurrently.
type ParallelExpr struct {
	Branches []Expr
	Pos      Position
}

func (p *ParallelExpr) Children() []Expr { return p.Branches }
func (*ParallelExpr) Node()              {}

// FallbackExpr: left || right.
type FallbackExpr struct {
	L, R Expr
	Pos  Position
}

func (*FallbackExpr) Node()              {}
func (f *FallbackExpr) Children() []Expr { return []Expr{f.L, f.R} }

// ConditionalExpr: cond ? then : else.
type ConditionalExpr struct {
	Cond Expr
	Then Expr
	Else Expr
	Pos  Position
}

func (*ConditionalExpr) Node() {}
func (c *ConditionalExpr) Children() []Expr {
	if c.Else == nil {
		return []Expr{c.Cond, c.Then}
	}
	return []Expr{c.Cond, c.Then, c.Else}
}

// ProjectionExpr: raw content `{ ... }`.
type ProjectionExpr struct {
	Raw       string
	Pos       Position
	TypeCheck TypedExpressionCheck
}

func (*ProjectionExpr) Node()              {}
func (p *ProjectionExpr) Children() []Expr { return nil }
func (*ProjectionExpr) ProjectionOutput()  {}

// LoopExpr: loop(body) until(condition).
type LoopExpr struct {
	Body  Expr
	Until string
	Pos   Position
}

func (*LoopExpr) Node()              {}
func (l *LoopExpr) Children() []Expr { return []Expr{l.Body} }

// AssertExpr: assert(condition, message).
type AssertExpr struct {
	Condition         string
	Message           string
	MessageExpression string
	Pos               Position
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
	ValueTyped
	ValueExpression
)

type Value struct {
	Kind           ValueKind
	Str            string
	preserveString bool // true when a resolved config/CLI string must stay a string
	Num            float64
	Bool           bool
	Ref            string
	Map            []MapEntry
	Slice          []*Value
	Typed          any
	Expression     string
	Program        ExpressionProgram
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

// DecoratedExpr wraps any primary with postfix modifiers, so constructs
// like `{ ... }:timeout=5s` and `loop(...) until(...):retry=3` behave
// like atoms for policy purposes. Build lowers Inner and applies the
// modifier chain through the same ModifierTable used for atoms.
type DecoratedExpr struct {
	Inner          Expr
	Modifiers      []string
	ModifierValues []ModifierValue
	Pos            Position
}

func (*DecoratedExpr) Node()              {}
func (d *DecoratedExpr) Children() []Expr { return []Expr{d.Inner} }
