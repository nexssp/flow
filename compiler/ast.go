package compiler

type Node interface{ node() }

type Expr interface {
	Node
	expr()
}

type PipelineExpr struct {
	Left  Expr
	Right Expr
}

func (*PipelineExpr) node() {}
func (*PipelineExpr) expr() {}

type ParallelExpr struct {
	Children []Expr
}

func (*ParallelExpr) node() {}
func (*ParallelExpr) expr() {}

// ConditionalExpr is `gate ? target` or `gate ? target : else`.
// Else is nil in the two-arm form.
type ConditionalExpr struct {
	Gate   Expr
	Target Expr
	Else   Expr
}

func (*ConditionalExpr) node() {}
func (*ConditionalExpr) expr() {}

type FallbackExpr struct {
	Left  Expr
	Right Expr
}

func (*FallbackExpr) node() {}
func (*FallbackExpr) expr() {}

type ProjectionExpr struct {
	Raw string
}

func (*ProjectionExpr) node() {}
func (*ProjectionExpr) expr() {}

type LoopExpr struct {
	Body  Expr
	Until string
}

func (*LoopExpr) node() {}
func (*LoopExpr) expr() {}

type AssertExpr struct {
	Condition string
	Message   string
}

func (*AssertExpr) node() {}
func (*AssertExpr) expr() {}

// AtomExpr is a single action reference in the pipeline.
//
// Modifiers come from the `:key=value` syntax. Params and Inputs come
// from the older `(key=value, ...)` syntax and remain for backward
// compatibility. Args come from `@{ key: value }` and carry full
// typed structure (see Value).
type AtomExpr struct {
	Name      string
	Profile   string
	Prompt    string
	Targets   []string
	Excludes  []string
	Params    map[string]string // from (key=value) — literal strings
	Inputs    map[string]string // from (key=value) where value has a dot
	Args      map[string]*Value // from @{ key: value } — typed
	Modifiers []string
}

func (*AtomExpr) node() {}
func (*AtomExpr) expr() {}

// ── Typed values for @{ ... } ────────────────────────────────────────

// ValueKind identifies the type of a parsed @{} argument.
type ValueKind uint8

const (
	ValueString ValueKind = iota
	ValueNumber
	ValueBool
	ValueNull
	ValueRef   // .foo.bar  → Ref = "foo.bar"
	ValueMap   // { key: value, ... }
	ValueSlice // [ a, b, c ]
)

// Value is a typed value inside an @{} block. It is the compile-time
// representation of a DSL argument: at execution the parser resolves
// it into a concrete Go value (string, float64, bool, map[string]any,
// []any, or a reference looked up in the pipeline state).
type Value struct {
	Kind  ValueKind
	Str   string     // ValueString
	Num   float64    // ValueNumber
	Bool  bool       // ValueBool
	Ref   string     // ValueRef: "user.name" (leading dot stripped)
	Map   []MapEntry // ValueMap: ordered, so error messages and diffs are stable
	Slice []*Value   // ValueSlice
}

// MapEntry is one key-value pair inside a nested map.
type MapEntry struct {
	Key   string
	Value *Value
}

// IsWholeStateRef reports whether this value is a bare "." reference,
// meaning "the entire state as it arrived". The compiler treats this
// specially: it snapshots the state before injecting other args, so
// the reference never captures a self-modifying map.
func (v *Value) IsWholeStateRef() bool {
	return v != nil && v.Kind == ValueRef && v.Ref == ""
}
