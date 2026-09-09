// nexssp/flow/compiler/ast.go
package compiler

type Node interface {
	node()
}

// Expr is the root of all expression nodes.
type Expr interface {
	Node
	expr()
}

// --- concrete nodes ---

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

type ConditionalExpr struct {
	Gate   Expr
	Target Expr
}

func (*ConditionalExpr) node() {}
func (*ConditionalExpr) expr() {}

type FallbackExpr struct {
	Left  Expr
	Right Expr
}

func (*FallbackExpr) node() {}
func (*FallbackExpr) expr() {}

// ProjectionExpr holds the raw content inside { ... }.
// We keep it as a raw string – expr-lang will parse it later.
type ProjectionExpr struct {
	Raw string // e.g. `prompt: "hello" + name`
}

func (*ProjectionExpr) node() {}
func (*ProjectionExpr) expr() {}

// LoopExpr represents an autonomous multi-turn bounded loop execution block
type LoopExpr struct {
	Body  Expr
	Until string
}

func (*LoopExpr) node() {}
func (*LoopExpr) expr() {}

type AtomExpr struct {
	Name      string            // e.g. "github.issue"
	Profile   string            // from :arch
	Prompt    string            // from @security
	Targets   []string          // from #pkg
	Excludes  []string          // from ~testdata
	Params    map[string]string // from (code=pack.content)
	Inputs    map[string]string // from (code=pack.content) – same as params but semantic
	Modifiers []string          // from :retry=3 :idempotent
}

func (*AtomExpr) node() {}
func (*AtomExpr) expr() {}
