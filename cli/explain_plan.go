package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner"
)

// ActionPlanReport is an AST-derived Flow lowering plan. It is deliberately
// separate from ActionGraphReport, which snapshots the program built by Kernel.
type ActionPlanReport struct {
	Path      string               `json:"path"`
	TopLevel  *ActionPlanGraph     `json:"top_level,omitempty"`
	Pipelines []ActionPlanPipeline `json:"pipelines,omitempty"`
}

// ActionPlanPipeline is the AST-derived plan for one @pipeline body.
type ActionPlanPipeline struct {
	Name  string          `json:"name"`
	Graph ActionPlanGraph `json:"graph"`
}

// ActionPlanGraph contains only control-flow structure and source positions.
// It intentionally excludes modifiers and action argument values.
type ActionPlanGraph struct {
	Entry string                     `json:"entry,omitempty"`
	Exit  string                     `json:"exit,omitempty"`
	Nodes []ActionPlanNode           `json:"nodes,omitempty"`
	Edges []compiler.ActionGraphEdge `json:"edges,omitempty"`
}

// ActionPlanNode describes a planned action without carrying request data.
type ActionPlanNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`

	// These static direct-field summaries are rendered only in Mermaid. They
	// are intentionally unexported so JSON/DOT retain their existing shape.
	projectionReads   []string
	projectionOutputs []string
}

type actionPlanOutputFormat uint8

const (
	actionPlanOutputText actionPlanOutputFormat = iota
	actionPlanOutputJSON
	actionPlanOutputDOT
	actionPlanOutputMermaid
)

const (
	actionPlanNodeKindConditional = "conditional"
	actionPlanNodeKindErrorGuard  = "error.guard"
	actionPlanNodeKindFallback    = "fallback"
	actionPlanNodeKindLoop        = "loop"
	actionPlanNodeKindLoopExit    = "loop.exit"
	actionPlanNodeKindMatch       = "match"
	actionPlanNodeKindMerge       = "merge"
)

func renderExplainActionPlan(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	src string,
	path string,
	jsonOut bool,
	dotOut bool,
) error {
	format := actionPlanOutputText
	if jsonOut {
		format = actionPlanOutputJSON
	} else if dotOut {
		format = actionPlanOutputDOT
	}
	return renderExplainActionPlanWithFormat(ctx, w, cfg, src, path, format)
}

func renderExplainActionPlanWithFormat(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	src string,
	path string,
	format actionPlanOutputFormat,
) error {
	return renderExplainActionPlanWithFormatAndTheme(ctx, w, cfg, src, path, format, actionPlanMermaidThemeLight)
}

func renderExplainActionPlanWithFormatAndTheme(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	src string,
	path string,
	format actionPlanOutputFormat,
	mermaidTheme string,
) error {
	topAST, pipelines, err := parseExplainASTWithContext(ctx, cfg, src, path)
	if err != nil {
		return err
	}
	report, err := actionPlanReport(cfg.Resolver, path, topAST, pipelines)
	if err != nil {
		return err
	}
	switch format {
	case actionPlanOutputJSON:
		return writeJSON(w, report, true)
	case actionPlanOutputDOT:
		return writeActionPlanDOT(w, report)
	case actionPlanOutputMermaid:
		return writeActionPlanMermaidWithTheme(w, report, mermaidTheme)
	case actionPlanOutputText:
		writeActionPlanText(w, report)
		return nil
	default:
		return fmt.Errorf("unsupported action plan output format %d", format)
	}
}

func actionPlanReport(
	resolver compiler.CapabilityResolver,
	path string,
	topAST compiler.Expr,
	pipelines map[string]compiler.Expr,
) (ActionPlanReport, error) {
	report := ActionPlanReport{Path: path}
	if topAST != nil {
		graph, err := buildActionPlanGraph(resolver, topAST)
		if err != nil {
			return ActionPlanReport{}, err
		}
		report.TopLevel = graph
	}
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		graph, err := buildActionPlanGraph(resolver, pipelines[name])
		if err != nil {
			return ActionPlanReport{}, err
		}
		report.Pipelines = append(report.Pipelines, ActionPlanPipeline{Name: name, Graph: *graph})
	}
	return report, nil
}

func buildActionPlanGraph(resolver compiler.CapabilityResolver, expr compiler.Expr) (*ActionPlanGraph, error) {
	graph, err := compiler.BuildActionGraph(resolver, expr)
	if err != nil {
		return nil, err
	}
	projectionFields := actionPlanProjectionFieldSummaries(expr)
	plan := &ActionPlanGraph{
		Entry: graph.Entry,
		Exit:  graph.Exit,
		Edges: append([]compiler.ActionGraphEdge(nil), graph.Edges...),
		Nodes: make([]ActionPlanNode, len(graph.Nodes)),
	}
	for i, node := range graph.Nodes {
		plannedNode := ActionPlanNode{
			ID: node.ID, Kind: node.Kind, Name: node.Name,
			File: node.File, Line: node.Line, Column: node.Column,
		}
		if node.Name == "projection.project" {
			position := actionPlanNodePosition{file: node.File, line: node.Line, column: node.Column}
			if fields, ok := projectionFields[position]; ok {
				plannedNode.projectionReads = fields.reads
				plannedNode.projectionOutputs = fields.outputs
			}
		}
		plan.Nodes[i] = plannedNode
	}
	return plan, nil
}

func writeActionPlanText(w io.Writer, report ActionPlanReport) {
	fmt.Fprintf(w, "%s\n\n", report.Path)
	fmt.Fprintln(w, "AST-derived Flow lowering plan (not the built Kernel action graph; argument values are omitted):")
	if report.TopLevel != nil {
		fmt.Fprintln(w, "\n[top-level]")
		renderActionPlanGraphText(w, report.TopLevel)
	}
	for _, pipeline := range report.Pipelines {
		fmt.Fprintf(w, "\n[@pipeline %s]\n", pipeline.Name)
		renderActionPlanGraphText(w, &pipeline.Graph)
	}
}

func renderActionPlanGraphText(w io.Writer, graph *ActionPlanGraph) {
	fmt.Fprintf(w, "entry: %s  exit: %s\n", graph.Entry, graph.Exit)
	fmt.Fprintln(w, "nodes:")
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		fmt.Fprintf(w, "  %s [%s] %s", node.ID, node.Kind, node.Name)
		if node.File != "" && node.Line > 0 {
			fmt.Fprintf(w, " @%s:%d:%d", node.File, node.Line, node.Column)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "edges:")
	for _, edge := range graph.Edges {
		fmt.Fprintf(w, "  %s -> %s [%s]\n", edge.From, edge.To, edge.Kind)
	}
}

func writeActionPlanDOT(w io.Writer, report ActionPlanReport) error {
	if _, err := fmt.Fprintln(w, "digraph action_plan {\n  rankdir=LR;"); err != nil {
		return err
	}
	cluster := 0
	if report.TopLevel != nil {
		cluster++
		writeActionPlanCluster(w, cluster, "top-level", report.TopLevel)
	}
	for _, pipeline := range report.Pipelines {
		cluster++
		writeActionPlanCluster(w, cluster, "@pipeline "+pipeline.Name, &pipeline.Graph)
	}
	_, err := fmt.Fprintln(w, "}")
	return err
}

func writeActionPlanCluster(w io.Writer, cluster int, label string, graph *ActionPlanGraph) {
	prefix := "p" + strconv.Itoa(cluster) + "_"
	fmt.Fprintf(w, "  subgraph cluster_%d {\n    label=%s;\n", cluster, strconv.Quote(label))
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		id := prefix + node.ID
		text := node.Kind
		if node.Name != "" {
			text += "\n" + node.Name
		}
		if node.Line > 0 {
			text += "\nline " + strconv.Itoa(node.Line)
		}
		fmt.Fprintf(w, "    %s [label=%s, shape=%s];\n", id, strconv.Quote(text), actionPlanShape(node.Kind))
	}
	for _, edge := range graph.Edges {
		fmt.Fprintf(w, "    %s%s -> %s%s [label=%s];\n",
			prefix, edge.From, prefix, edge.To, strconv.Quote(edge.Kind))
	}
	fmt.Fprintln(w, "  }")
}

func actionPlanShape(kind string) string {
	switch kind {
	case "parallel.fork", actionPlanNodeKindConditional, actionPlanNodeKindFallback, actionPlanNodeKindMatch, actionPlanNodeKindErrorGuard, actionPlanNodeKindLoop:
		return "diamond"
	case actionPlanNodeKindMerge, actionPlanNodeKindLoopExit:
		return "circle"
	default:
		return "box"
	}
}
