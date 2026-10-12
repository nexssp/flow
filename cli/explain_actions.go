package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner"
)

type ActionGraphReport struct {
	SchemaVersion int                        `json:"schema_version"`
	Path          string                     `json:"-"`
	Program       *action.IntrospectionGraph `json:"program,omitempty"`
	Pipelines     []ActionGraphPipeline      `json:"pipelines,omitempty"`
}

type ActionGraphPipeline struct {
	Name  string                    `json:"name"`
	Graph action.IntrospectionGraph `json:"graph"`
}

func renderExplainActionGraph(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	src string,
	path string,
	jsonOut bool,
	dotOut bool,
) error {
	compiled, err := runner.CompileWithOptions(ctx, cfg, src, path, compiler.WithActionIntrospection())
	if err != nil {
		return err
	}
	report := actionGraphReport(compiled, path)
	if jsonOut {
		return writeJSON(w, report, true)
	}
	if dotOut {
		return writeActionGraphDOT(w, report)
	}
	return writeActionGraphText(w, report)
}

func actionGraphReport(compiled runner.Compilation, path string) ActionGraphReport {
	report := ActionGraphReport{SchemaVersion: action.IntrospectionSchemaVersion, Path: path}
	if compiled.Program != nil {
		graph := action.InspectActionGraph(compiled.Program)
		report.Program = &graph
	}
	pipelines, _ := compiled.Meta["pipelines"].(map[string]string)
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		canonical := name
		if !strings.Contains(canonical, ".") {
			canonical = "pipeline." + canonical
		}
		program, ok := compiled.Resolver.Action(canonical)
		if !ok {
			continue
		}
		report.Pipelines = append(report.Pipelines, ActionGraphPipeline{
			Name: name, Graph: action.InspectActionGraph(program),
		})
	}
	return report
}

func writeActionGraphText(w io.Writer, report ActionGraphReport) error {
	if _, err := fmt.Fprintf(w, "%s\n\n", report.Path); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Built Kernel action graph (schema v%d; argument values are omitted):\n", report.SchemaVersion); err != nil {
		return err
	}
	if report.Program != nil {
		if _, err := fmt.Fprintln(w, "\n[program]"); err != nil {
			return err
		}
		if err := renderActionGraphText(w, *report.Program); err != nil {
			return err
		}
	}
	for _, pipeline := range report.Pipelines {
		if _, err := fmt.Fprintf(w, "\n[@pipeline %s]\n", pipeline.Name); err != nil {
			return err
		}
		if err := renderActionGraphText(w, pipeline.Graph); err != nil {
			return err
		}
	}
	return nil
}

func renderActionGraphText(w io.Writer, graph action.IntrospectionGraph) error {
	if _, err := fmt.Fprintf(w, "root: %s\n", graph.Root); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "nodes:"); err != nil {
		return err
	}
	if err := renderActionGraphNodes(w, graph.Nodes); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "edges:"); err != nil {
		return err
	}
	if err := renderActionGraphEdges(w, graph.Edges); err != nil {
		return err
	}
	if graph.Truncated {
		_, err := fmt.Fprintln(w, "graph truncated by introspection safety limits")
		return err
	}
	return nil
}

func renderActionGraphNodes(w io.Writer, nodes []action.IntrospectionGraphNode) error {
	for i := range nodes {
		if err := renderActionGraphNode(w, &nodes[i]); err != nil {
			return err
		}
	}
	return nil
}

func renderActionGraphNode(w io.Writer, node *action.IntrospectionGraphNode) error {
	if _, err := fmt.Fprintf(w, "  %s [%s] %s", node.ID, node.Kind, node.Name); err != nil {
		return err
	}
	if node.Description != "" {
		if _, err := fmt.Fprintf(w, " — %s", node.Description); err != nil {
			return err
		}
	}
	if len(node.Tags) > 0 {
		if _, err := fmt.Fprintf(w, " [tags=%s]", strings.Join(node.Tags, ",")); err != nil {
			return err
		}
	}
	if node.Truncated {
		if _, err := fmt.Fprint(w, " [truncated]"); err != nil {
			return err
		}
	}
	if node.Opaque {
		if _, err := fmt.Fprint(w, " [opaque]"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, note := range node.Notes {
		if _, err := fmt.Fprintf(w, "    note: %s\n", note); err != nil {
			return err
		}
	}
	return nil
}

func renderActionGraphEdges(w io.Writer, edges []action.IntrospectionEdge) error {
	for i := range edges {
		if err := renderActionGraphEdge(w, &edges[i]); err != nil {
			return err
		}
	}
	return nil
}

func renderActionGraphEdge(w io.Writer, edge *action.IntrospectionEdge) error {
	if _, err := fmt.Fprintf(w, "  %s -[%s", edge.From, edge.Relation); err != nil {
		return err
	}
	if edge.Role != "" {
		if _, err := fmt.Fprintf(w, "; role=%s", edge.Role); err != nil {
			return err
		}
	}
	if edge.Label != "" {
		if _, err := fmt.Fprintf(w, "; label=%q", edge.Label); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "; order=%d]-> %s\n", edge.Order, edge.To)
	return err
}

func writeActionGraphDOT(w io.Writer, report ActionGraphReport) error {
	if _, err := fmt.Fprintln(w, "digraph compiled_program {\n  rankdir=LR;"); err != nil {
		return err
	}
	cluster := 0
	if report.Program != nil {
		cluster++
		if err := writeActionGraphCluster(w, cluster, "program", *report.Program); err != nil {
			return err
		}
	}
	for _, pipeline := range report.Pipelines {
		cluster++
		if err := writeActionGraphCluster(w, cluster, "@pipeline "+pipeline.Name, pipeline.Graph); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, "}")
	return err
}

func writeActionGraphCluster(w io.Writer, cluster int, label string, graph action.IntrospectionGraph) error {
	prefix := "g" + strconv.Itoa(cluster) + "_"
	if _, err := fmt.Fprintf(w, "  subgraph cluster_%d {\n    label=%s;\n", cluster, strconv.Quote(label)); err != nil {
		return err
	}
	if err := writeActionGraphDOTNodes(w, prefix, graph.Nodes); err != nil {
		return err
	}
	if err := writeActionGraphDOTEdges(w, prefix, graph.Edges); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "  }")
	return err
}

func writeActionGraphDOTNodes(w io.Writer, prefix string, nodes []action.IntrospectionGraphNode) error {
	for i := range nodes {
		node := &nodes[i]
		var b strings.Builder
		b.WriteString(node.Kind)
		if node.Name != "" {
			b.WriteString("\n")
			b.WriteString(node.Name)
		}
		if node.Description != "" {
			b.WriteString("\n")
			b.WriteString(node.Description)
		}
		if len(node.Tags) > 0 {
			b.WriteString("\n")
			b.WriteString(strings.Join(node.Tags, ","))
		}
		if node.Truncated {
			b.WriteString("\ntruncated")
		}
		if node.Opaque {
			b.WriteString("\nopaque")
		}
		for _, note := range node.Notes {
			b.WriteString("\n")
			b.WriteString(note)
		}
		if _, err := fmt.Fprintf(w, "    %s%s [label=%s];\n", prefix, node.ID, strconv.Quote(b.String())); err != nil {
			return err
		}
	}
	return nil
}

func writeActionGraphDOTEdges(w io.Writer, prefix string, edges []action.IntrospectionEdge) error {
	for i := range edges {
		edge := &edges[i]
		var b strings.Builder
		b.WriteString(edge.Relation)
		if edge.Role != "" {
			b.WriteString(":")
			b.WriteString(edge.Role)
		}
		if edge.Label != "" {
			b.WriteString(" ")
			b.WriteString(edge.Label)
		}
		b.WriteString(" #")
		b.WriteString(strconv.Itoa(edge.Order))
		if _, err := fmt.Fprintf(w, "    %s%s -> %s%s [label=%s];\n",
			prefix, edge.From, prefix, edge.To, strconv.Quote(b.String())); err != nil {
			return err
		}
	}
	return nil
}
