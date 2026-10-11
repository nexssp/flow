package compiler

import (
	"strconv"
	"strings"
)

// ActionGraph is a reviewable, high-level plan of the actions and control/data
// relationships produced from one expression. It deliberately contains no
// action arguments or parameter values. Kernel action implementations remain
// opaque; this graph describes Flow's lowering decisions, not their internals.
type ActionGraph struct {
	Entry string            `json:"entry,omitempty"`
	Exit  string            `json:"exit,omitempty"`
	Nodes []ActionGraphNode `json:"nodes,omitempty"`
	Edges []ActionGraphEdge `json:"edges,omitempty"`
}

// ActionGraphNode is one action or synthetic control node in an ActionGraph.
type ActionGraphNode struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name,omitempty"`
	File      string   `json:"file,omitempty"`
	Line      int      `json:"line,omitempty"`
	Column    int      `json:"column,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

// ActionGraphEdge connects action outputs, branch routes, or loop control.
type ActionGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// ActionGraphForm describes the control shape of an extension expression.
type ActionGraphForm uint8

const (
	// ActionGraphOpaque marks an extension whose execution shape is not declared.
	ActionGraphOpaque ActionGraphForm = iota
	// ActionGraphParallel represents concurrent branches with a join.
	ActionGraphParallel
	// ActionGraphChoice represents mutually exclusive branches with a join.
	ActionGraphChoice
	// ActionGraphCatch represents protected work with recovery branches.
	ActionGraphCatch
)

// ActionGraphDescription lets an expression extension describe its plan node
// and the labels for its Children. Labels should describe branch roles, not
// include runtime values or secrets.
type ActionGraphDescription struct {
	Kind        string
	Name        string
	Form        ActionGraphForm
	ChildLabels []string
}

// ActionGraphProvider is an optional Expr extension contract. Expressions that
// do not implement it are shown as opaque extension nodes with contained child
// plans, rather than guessed execution semantics.
type ActionGraphProvider interface {
	DescribeActionGraph() ActionGraphDescription
}

// BuildActionGraph builds the deterministic, high-level action graph for expr.
// It is only run when explicitly requested; normal compilation adds no graph
// construction or allocation. Action and stream parameters are never copied.
func BuildActionGraph(resolver CapabilityResolver, expr Expr) (*ActionGraph, error) {
	graph := &ActionGraph{}
	if expr == nil {
		return graph, nil
	}

	builder := actionGraphBuilder{graph: graph, resolver: resolver}
	segment, err := builder.build(expr)
	if err != nil {
		return nil, err
	}
	graph.Entry = segment.entry
	graph.Exit = segment.exit
	return graph, nil
}

type actionGraphSegment struct {
	entry string
	exit  string
}

type actionGraphBuilder struct {
	graph    *ActionGraph
	resolver CapabilityResolver
}

func (b *actionGraphBuilder) build(expr Expr) (actionGraphSegment, error) {
	if expr == nil {
		return actionGraphSegment{}, nil
	}

	switch node := expr.(type) {
	case *Atom:
		return b.buildAtom(node), nil
	case *PipeExpr:
		return b.buildPipe(node)
	case *ParallelExpr:
		return b.buildParallel("parallel", "parallel", node.Pos, node.Branches, nil)
	case *FallbackExpr:
		return b.buildFallback(node)
	case *ConditionalExpr:
		return b.buildConditional(node)
	case *ProjectionExpr:
		return b.addAction("projection.project", node.Pos, nil), nil
	case *AssertExpr:
		return b.addAction("assert", node.Pos, nil), nil
	case *LoopExpr:
		return b.buildLoop(node)
	case *DecoratedExpr:
		return b.buildDecorated(node)
	default:
		return b.buildExtension(expr)
	}
}

func (b *actionGraphBuilder) buildAtom(atom *Atom) actionGraphSegment {
	kind := IntrospectionKindAction
	if b.resolver != nil {
		switch {
		case isStreamSource(b.resolver, atom.Name):
			kind = "stream.source"
		case isStreamOperator(b.resolver, atom.Name):
			kind = "stream.operator"
		case isStreamBoundary(atom.Name):
			kind = "stream.boundary"
		}
	}
	return b.addNode(kind, atom.Name, atom.Pos, modifierNames(atom.Modifiers))
}

func isStreamSource(resolver CapabilityResolver, name string) bool {
	_, ok := resolver.Stream(name)
	return ok
}

func isStreamOperator(resolver CapabilityResolver, name string) bool {
	_, ok := resolver.Operator(name)
	return ok
}

func isStreamBoundary(name string) bool {
	_, ok := BoundaryByName(name)
	return ok
}

func (b *actionGraphBuilder) buildPipe(pipe *PipeExpr) (actionGraphSegment, error) {
	var result actionGraphSegment
	for _, expr := range flattenPipe(pipe) {
		segment, err := b.build(expr)
		if err != nil {
			return actionGraphSegment{}, err
		}
		if segment.entry == "" {
			continue
		}
		if result.entry == "" {
			result = segment
			continue
		}
		b.addEdge(result.exit, segment.entry, "data")
		result.exit = segment.exit
	}
	return result, nil
}

func (b *actionGraphBuilder) buildParallel(
	kind, name string,
	pos Position,
	children []Expr,
	labels []string,
) (actionGraphSegment, error) {
	fork := b.addNodeID(kind+".fork", name, pos, nil)
	join := b.addNodeID("merge", name+".result", pos, nil)
	for i, child := range children {
		segment, err := b.build(child)
		if err != nil {
			return actionGraphSegment{}, err
		}
		if segment.entry == "" {
			continue
		}
		label := childLabel(labels, i, IntrospectionKindBranch)
		b.addEdge(fork, segment.entry, label)
		b.addEdge(segment.exit, join, "result:"+label)
	}
	return actionGraphSegment{entry: fork, exit: join}, nil
}

func (b *actionGraphBuilder) buildFallback(expr *FallbackExpr) (actionGraphSegment, error) {
	gate := b.addNodeID("fallback", "first-success", expr.Pos, nil)
	join := b.addNodeID("merge", "fallback.result", expr.Pos, nil)
	left, err := b.build(expr.L)
	if err != nil {
		return actionGraphSegment{}, err
	}
	right, err := b.build(expr.R)
	if err != nil {
		return actionGraphSegment{}, err
	}
	b.addEdge(gate, left.entry, "first-attempt")
	b.addEdge(gate, right.entry, "on-failure")
	b.addEdge(left.exit, join, "success")
	b.addEdge(right.exit, join, "recovered")
	return actionGraphSegment{entry: gate, exit: join}, nil
}

func (b *actionGraphBuilder) buildConditional(expr *ConditionalExpr) (actionGraphSegment, error) {
	condition, err := b.build(expr.Cond)
	if err != nil {
		return actionGraphSegment{}, err
	}
	gate := b.addNodeID("conditional", "conditional", expr.Pos, nil)
	join := b.addNodeID("merge", "conditional.result", expr.Pos, nil)
	thenBranch, err := b.build(expr.Then)
	if err != nil {
		return actionGraphSegment{}, err
	}
	elseBranch, err := b.build(expr.Else)
	if err != nil {
		return actionGraphSegment{}, err
	}
	b.addEdge(condition.exit, gate, "condition")
	b.addEdge(gate, thenBranch.entry, "true")
	b.addEdge(thenBranch.exit, join, "result:true")
	if elseBranch.entry == "" {
		b.addEdge(gate, join, "false:passthrough")
	} else {
		b.addEdge(gate, elseBranch.entry, "false")
		b.addEdge(elseBranch.exit, join, "result:false")
	}
	return actionGraphSegment{entry: condition.entry, exit: join}, nil
}

func (b *actionGraphBuilder) buildLoop(expr *LoopExpr) (actionGraphSegment, error) {
	loop := b.addNodeID("loop", "until-condition", expr.Pos, nil)
	done := b.addNodeID("loop.exit", "loop.complete", expr.Pos, nil)
	body, err := b.build(expr.Body)
	if err != nil {
		return actionGraphSegment{}, err
	}
	b.addEdge(loop, body.entry, "first-iteration")
	b.addEdge(body.exit, loop, "check-until")
	b.addEdge(loop, body.entry, "repeat-if-false")
	b.addEdge(loop, done, "finish-if-true")
	return actionGraphSegment{entry: loop, exit: done}, nil
}

func (b *actionGraphBuilder) buildDecorated(expr *DecoratedExpr) (actionGraphSegment, error) {
	modifiers := modifierNames(expr.Modifiers)
	if len(modifiers) == 0 {
		return b.build(expr.Inner)
	}
	wrapper := b.addNodeID("modifier", strings.Join(modifiers, ","), expr.Pos, modifiers)
	inner, err := b.build(expr.Inner)
	if err != nil {
		return actionGraphSegment{}, err
	}
	if inner.entry == "" {
		return actionGraphSegment{entry: wrapper, exit: wrapper}, nil
	}
	b.addEdge(wrapper, inner.entry, "wraps")
	return actionGraphSegment{entry: wrapper, exit: inner.exit}, nil
}

func (b *actionGraphBuilder) buildExtension(expr Expr) (actionGraphSegment, error) {
	description, ok := expr.(ActionGraphProvider)
	if !ok {
		return b.buildOpaque(expr)
	}
	spec := description.DescribeActionGraph()
	switch spec.Form {
	case ActionGraphParallel:
		return b.buildParallel(spec.Kind, spec.Name, expressionPosition(expr), expr.Children(), spec.ChildLabels)
	case ActionGraphChoice:
		return b.buildChoice(spec, expr)
	case ActionGraphCatch:
		return b.buildCatch(spec, expr)
	case ActionGraphOpaque:
		return b.buildOpaque(expr)
	default:
		return b.buildOpaque(expr)
	}
}

func (b *actionGraphBuilder) buildChoice(
	spec ActionGraphDescription,
	expr Expr,
) (actionGraphSegment, error) {
	gate := b.addNodeID(spec.Kind, spec.Name, expressionPosition(expr), nil)
	join := b.addNodeID("merge", spec.Name+".result", expressionPosition(expr), nil)
	for i, child := range expr.Children() {
		segment, err := b.build(child)
		if err != nil {
			return actionGraphSegment{}, err
		}
		if segment.entry == "" {
			continue
		}
		label := childLabel(spec.ChildLabels, i, "case")
		b.addEdge(gate, segment.entry, label)
		b.addEdge(segment.exit, join, "result:"+label)
	}
	return actionGraphSegment{entry: gate, exit: join}, nil
}

func (b *actionGraphBuilder) buildCatch(
	spec ActionGraphDescription,
	expr Expr,
) (actionGraphSegment, error) {
	children := expr.Children()
	if len(children) == 0 {
		return b.addNode("extension", spec.Name, expressionPosition(expr), nil), nil
	}
	gate := b.addNodeID(spec.Kind, spec.Name, expressionPosition(expr), nil)
	join := b.addNodeID("merge", spec.Name+".result", expressionPosition(expr), nil)
	protected, err := b.build(children[0])
	if err != nil {
		return actionGraphSegment{}, err
	}
	b.addEdge(gate, protected.entry, "protected")
	b.addEdge(protected.exit, join, "success")
	for i, child := range children[1:] {
		segment, buildErr := b.build(child)
		if buildErr != nil {
			return actionGraphSegment{}, buildErr
		}
		if segment.entry == "" {
			continue
		}
		label := childLabel(spec.ChildLabels, i+1, "recovery")
		b.addEdge(protected.exit, segment.entry, label)
		b.addEdge(segment.exit, join, "recovered:"+label)
	}
	return actionGraphSegment{entry: gate, exit: join}, nil
}

func (b *actionGraphBuilder) buildOpaque(expr Expr) (actionGraphSegment, error) {
	name := "extension"
	if provider, ok := expr.(interface{ Name() string }); ok && provider.Name() != "" {
		name = provider.Name()
	}
	root := b.addNodeID("extension.opaque", name, expressionPosition(expr), nil)
	for i, child := range expr.Children() {
		segment, err := b.build(child)
		if err != nil {
			return actionGraphSegment{}, err
		}
		if segment.entry != "" {
			b.addEdge(root, segment.entry, "contains:"+strconv.Itoa(i+1))
		}
	}
	return actionGraphSegment{entry: root, exit: root}, nil
}

func (b *actionGraphBuilder) addAction(name string, pos Position, modifiers []string) actionGraphSegment {
	return b.addNode(IntrospectionKindAction, name, pos, modifiers)
}

func (b *actionGraphBuilder) addNode(kind, name string, pos Position, modifiers []string) actionGraphSegment {
	id := b.addNodeID(kind, name, pos, modifiers)
	return actionGraphSegment{entry: id, exit: id}
}

func (b *actionGraphBuilder) addNodeID(kind, name string, pos Position, modifiers []string) string {
	id := "n" + strconv.Itoa(len(b.graph.Nodes)+1)
	b.graph.Nodes = append(b.graph.Nodes, ActionGraphNode{
		ID:        id,
		Kind:      kind,
		Name:      name,
		File:      pos.File,
		Line:      pos.Line,
		Column:    pos.Col,
		Modifiers: modifiers,
	})
	return id
}

func (b *actionGraphBuilder) addEdge(from, to, kind string) {
	if from == "" || to == "" {
		return
	}
	b.graph.Edges = append(b.graph.Edges, ActionGraphEdge{From: from, To: to, Kind: kind})
}

func childLabel(labels []string, index int, prefix string) string {
	if index < len(labels) && labels[index] != "" {
		return labels[index]
	}
	return prefix + " " + strconv.Itoa(index+1)
}

func modifierNames(modifiers []string) []string {
	if len(modifiers) == 0 {
		return nil
	}
	out := make([]string, 0, len(modifiers))
	for _, modifier := range modifiers {
		name, _, _ := strings.Cut(strings.TrimSpace(modifier), "=")
		name = strings.TrimLeft(name, ":")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func expressionPosition(expr Expr) Position {
	switch node := expr.(type) {
	case *Atom:
		return node.Pos
	case *PipeExpr:
		return node.Pos
	case *ParallelExpr:
		return node.Pos
	case *FallbackExpr:
		return node.Pos
	case *ConditionalExpr:
		return node.Pos
	case *ProjectionExpr:
		return node.Pos
	case *AssertExpr:
		return node.Pos
	case *LoopExpr:
		return node.Pos
	case *DecoratedExpr:
		return node.Pos
	default:
		return Position{}
	}
}
