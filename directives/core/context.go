package core

import (
	"github.com/nexssp/flow/compiler"
)

// Position is the source location of a directive or atom, used in
// error messages. Same type as the compiler's Position so positions
// flow through both layers without conversion.
type Position = compiler.Position

// Context is the per-file state a directive sees during Apply.
//
// Body is the file split into lines. Directives that consume a block
// (via SplitBlock) leave Body untouched. Directives that rewrite a
// single line can write back into Body[i].
//
// ValidateProfile and IncludeResolver are nil in byte-source mode.
type Context struct {
	Out     *Preprocessed
	BaseDir string
	File    string
	Body    []string

	ValidateProfile func(string) error
	IncludeResolver func(absPath string) (*Preprocessed, error)
}

// Preprocessed is the parsed representation of one .flow file.
//
// Typed fields cover the shape every directive must agree on. Anything
// a single directive owns goes into Declarations, keyed by directive
// name. That keeps Preprocessed stable when a new directive is added.
type Preprocessed struct {
	File string

	Description string
	Asserts     []AssertDecl

	Profile      string
	ProfilePos   Position
	Requires     []Requirement
	Capabilities CapabilityEnvelope

	Gates []GateRule

	Pools []Pool

	Hooks []HookDecl

	Config map[string]string

	Includes []string

	Pipelines []Pipeline

	Action *ActionMeta

	Declarations map[string]any

	DSL string
}

type AssertDecl struct {
	Expr string
	Pos  Position
}

type HookDecl struct {
	Name string
	Pos  Position
}

type GateRule struct {
	Kind string
	Expr string
	Pos  Position
}

type Pool struct {
	Name    string
	Members []string
	Pos     Position
}

type CapabilityEnvelope struct {
	LLM     []string
	Sandbox []string
	Network []string
	Pos     Position
}

type Pipeline struct {
	Name string
	Body string
	Pos  Position
}

type ActionMeta struct {
	Name        string
	Description string
	Pos         Position
}

type Requirement struct {
	Import     string
	Version    string
	LocalPath  string
	ModuleRoot string
	ModulePath string
	Options    map[string]string
	Pos        Position
}

func (r Requirement) IsLocal() bool { return r.LocalPath != "" }

// OnErrorDecl is the parsed `@on_error { ... }` block.
type OnErrorDecl struct {
	Pos   Position
	Rules []OnErrorRule
	Else  string
}

// OnErrorRule is one `when COND -> TARGET` clause.
type OnErrorRule struct {
	Pos              Position
	MatchesSuspended bool
	MatchesKind      string
	Target           string
}
