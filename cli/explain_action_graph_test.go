package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/match"
	"github.com/nexssp/flow/extensions/on_error"
	"github.com/nexssp/flow/extensions/pool"
	"github.com/nexssp/flow/runner"
)

func TestParseExplainOptions_ActionsAndFlagOrder(t *testing.T) {
	options, err := parseExplainOptions([]string{"--actions", "--dot", "sample.nflow"})
	if err != nil {
		t.Fatal(err)
	}
	if options.path != "sample.nflow" || !options.actions || !options.dot {
		t.Fatalf("parsed options = %#v", options)
	}
	options, err = parseExplainOptions([]string{"sample.nflow", "--ast", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.path != "sample.nflow" || !options.ast || !options.json {
		t.Fatalf("parsed options = %#v", options)
	}
	options, err = parseExplainOptions([]string{"--plan", "--dot", "sample.nflow"})
	if err != nil {
		t.Fatal(err)
	}
	if options.path != "sample.nflow" || !options.plan || !options.dot {
		t.Fatalf("parsed options = %#v", options)
	}
}

func TestParseExplainOptions_RejectsConflictingViews(t *testing.T) {
	_, err := parseExplainOptions([]string{"sample.nflow", "--ast", "--actions"})
	if err == nil {
		t.Fatal("expected conflicting views to be rejected")
	}
	if _, err := parseExplainOptions([]string{"sample.nflow", "--actions", "--plan"}); err == nil {
		t.Fatal("expected action graph and AST plan views to conflict")
	}
}

func TestExplainActionGraph_TextJSONAndDOT(t *testing.T) {
	cfg := explainConfig(t)
	source := `noop @{ authorization: "must-not-appear" } -> noop`
	var text bytes.Buffer
	if err := renderExplainActionGraph(context.Background(), &text, cfg, source, "test.nflow", false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "Built Kernel action graph") || !strings.Contains(text.String(), "pipe") || !strings.Contains(text.String(), "runtime.noop") || !strings.Contains(text.String(), compiler.IntrospectionKindSequence) {
		t.Fatalf("text output is missing the built action graph: %s", text.String())
	}
	if strings.Contains(text.String(), "must-not-appear") || strings.Contains(text.String(), "authorization") {
		t.Fatal("text output leaked an argument name or value")
	}

	var jsonOut bytes.Buffer
	if err := renderExplainActionGraph(context.Background(), &jsonOut, cfg, source, "test.nflow", true, false); err != nil {
		t.Fatal(err)
	}
	var report ActionGraphReport
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON action graph: %v\n%s", err, jsonOut.String())
	}
	if report.SchemaVersion != action.IntrospectionSchemaVersion || report.Program == nil {
		t.Fatalf("JSON output is missing the graph envelope: %#v", report)
	}
	if report.Program.Nodes[0].Name != "pipe" || report.Program.Nodes[0].Kind != compiler.IntrospectionKindSequence {
		t.Fatalf("JSON output is missing the built composition: %#v", report.Program)
	}
	rootEdges := graphEdgesFrom(*report.Program, report.Program.Root)
	if len(rootEdges) != 2 || rootEdges[0].Role != "left" || rootEdges[1].Role != "right" || rootEdges[0].Order != 0 || rootEdges[1].Order != 1 {
		t.Fatalf("JSON output lost pipe relationships: %#v", rootEdges)
	}
	if strings.Contains(jsonOut.String(), "must-not-appear") || strings.Contains(jsonOut.String(), "authorization") || strings.Contains(jsonOut.String(), "test.nflow") {
		t.Fatal("JSON output leaked an argument, argument name, or source path")
	}

	var dot bytes.Buffer
	if err := renderExplainActionGraph(context.Background(), &dot, cfg, source, "test.nflow", false, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dot.String(), "digraph compiled_program") || !strings.Contains(dot.String(), "sequence:left #0") || !strings.Contains(dot.String(), "->") {
		t.Fatalf("DOT output is missing labeled graph edges: %s", dot.String())
	}
}

func TestExplainActionGraph_UsesMaterializedPipelineProgram(t *testing.T) {
	cfg := explainConfig(t)
	source := `@pipeline work
  noop -> noop
@end
pipeline.work`
	var output bytes.Buffer
	if err := renderExplainActionGraph(context.Background(), &output, cfg, source, "pipeline.nflow", true, false); err != nil {
		t.Fatal(err)
	}
	var report ActionGraphReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode action report: %v\n%s", err, output.String())
	}
	if len(report.Pipelines) != 1 || report.Pipelines[0].Name != "work" {
		t.Fatalf("materialized pipeline missing from report: %#v", report.Pipelines)
	}
	if report.Program == nil || report.Program.Nodes[0].Kind != "pipeline" {
		t.Fatalf("top-level compiled Program does not expose the materialized pipeline: %#v", report.Program)
	}
	programEdges := graphEdgesFrom(*report.Program, report.Program.Root)
	if len(programEdges) != 1 || programEdges[0].Relation != "pipeline_body" {
		t.Fatalf("compiled Program is missing its pipeline body edge: %#v", programEdges)
	}
	if node := findGraphNode(*report.Program, programEdges[0].To); node == nil || node.Kind != "sequence" {
		t.Fatalf("compiled Program pipeline body is missing: %#v", report.Program.Nodes)
	}
	graph := report.Pipelines[0].Graph
	if len(graph.Nodes) < 4 || graph.Nodes[0].Name != "pipeline.work" || graph.Nodes[0].Kind != "pipeline" || len(graph.Edges) < 3 {
		t.Fatalf("report did not inspect the materialized pipeline program: %#v", graph)
	}
	var bodyID string
	for _, edge := range graph.Edges {
		if edge.From == graph.Root && edge.Relation == "pipeline_body" && edge.Role == "body" {
			bodyID = edge.To
			break
		}
	}
	if node := findGraphNode(graph, bodyID); node == nil || node.Name != "pipe" || node.Kind != compiler.IntrospectionKindSequence {
		t.Fatalf("materialized pipeline body edge is missing: %#v", graph)
	}
}

func TestExplainActionGraph_DescribesNamedParallelBranches(t *testing.T) {
	source := `parallel {
  profile: runtime.const @{ value: "profile-secret" },
  metrics: runtime.const @{ value: "metrics-secret" },
}`
	graph := compileExplainGraph(t, explainConfig(t), source)
	if graph.Nodes[0].Kind != "parallel.named" {
		t.Fatalf("root kind = %q, want parallel.named", graph.Nodes[0].Kind)
	}
	edges := graphEdgesFrom(graph, graph.Root)
	if len(edges) != 2 || edges[0].Relation != compiler.IntrospectionKindBranch || edges[0].Label != "profile" || edges[1].Label != "metrics" {
		t.Fatalf("named parallel branches are not labeled: %#v", edges)
	}
}

func TestExplainActionGraph_DescribesMatchWithoutConditions(t *testing.T) {
	cfg := explainConfigWithBundles(t, match.Bundle(nil))
	source := `const @{ value: { status: "ACTIVE" } }
-> match(.status) {
  "ACTIVE" -> const @{ value: "running-secret" },
  _ -> const @{ value: "other-secret" }
}`
	graph := compileExplainGraph(t, cfg, source)
	matchNode := findGraphNodeKind(graph, "match")
	if matchNode == nil || !matchNode.Opaque {
		t.Fatalf("match control node is missing or not marked opaque: %#v", graph.Nodes)
	}
	edges := graphEdgesFrom(graph, matchNode.ID)
	if len(edges) != 2 || edges[0].Role != "case" || edges[1].Role != "default" {
		t.Fatalf("match branch roles are missing: %#v", edges)
	}
}

func TestExplainActionGraph_DescribesErrorRecoveryBranches(t *testing.T) {
	cfg := explainConfigWithBundles(t, on_error.Bundle(nil))
	source := `fail @{ kind: "Unavailable", message: "db node is down" } ? on_error {
  xerr.KindUnavailable -> runtime.const @{ value: "recovered-secret" },
  else -> runtime.const @{ value: "fallback-secret" }
}`
	graph := compileExplainGraph(t, cfg, source)
	node := findGraphNodeKind(graph, "error_recovery")
	if node == nil || !node.Opaque {
		t.Fatalf("error-recovery node is missing or not marked opaque: %#v", graph.Nodes)
	}
	edges := graphEdgesFrom(graph, node.ID)
	if len(edges) != 3 || edges[0].Relation != "protects" || edges[1].Relation != "recovers_to" || edges[2].Label != "else" {
		t.Fatalf("error-recovery roles are missing: %#v", edges)
	}
}

func TestExplainActionGraph_PoolContainsActualResolvedMembers(t *testing.T) {
	cfg := explainConfigWithBundles(t, pool.Bundle(nil))
	graph := compileExplainGraph(t, cfg, "@pool coverage [noop]\npool.coverage")
	node := findGraphNodeKind(graph, "pool.round_robin")
	if node == nil {
		t.Fatalf("materialized pool action is missing from built graph: %#v", graph.Nodes)
	}
	edges := graphEdgesFrom(graph, node.ID)
	if len(edges) != 1 || edges[0].Relation != "route" || !strings.Contains(edges[0].Label, "noop") {
		t.Fatalf("pool route does not point to its resolved member: %#v", edges)
	}
}

func TestCompileActionIntrospectionIsOptIn(t *testing.T) {
	cfg := explainConfig(t)
	plain, err := runner.Compile(context.Background(), cfg, "noop -> noop", "plain.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Program.(action.Composite); ok {
		t.Fatal("ordinary compilation unexpectedly retained action children")
	}

	inspected, err := runner.CompileWithOptions(context.Background(), cfg, "noop -> noop", "graph.nflow", compiler.WithActionIntrospection())
	if err != nil {
		t.Fatal(err)
	}
	graph := action.InspectActionGraph(inspected.Program)
	if len(graph.Nodes) != 3 || graph.Nodes[0].Kind != compiler.IntrospectionKindSequence || len(graph.Edges) != 2 {
		t.Fatalf("unexpected opted-in action graph: %#v", graph)
	}
}

func TestExplainActionPlan_IsSeparateAndOmitsModifiers(t *testing.T) {
	cfg := explainConfig(t)
	source := `noop @{ authorization: "must-not-appear" } -> noop`
	var output bytes.Buffer
	if err := renderExplainActionPlan(context.Background(), &output, cfg, source, "plan.nflow", true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"top_level"`) || strings.Contains(output.String(), "must-not-appear") || strings.Contains(output.String(), "authorization") {
		t.Fatalf("AST plan missing structure or exposing arguments: %s", output.String())
	}
	var report ActionPlanReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode plan report: %v\n%s", err, output.String())
	}
	if report.TopLevel == nil || len(report.TopLevel.Nodes) == 0 {
		t.Fatalf("AST plan missing nodes: %#v", report.TopLevel)
	}
	var text bytes.Buffer
	if err := renderExplainActionPlan(context.Background(), &text, cfg, source, "plan.nflow", false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "AST-derived Flow lowering plan") || !strings.Contains(text.String(), "not the built Kernel action graph") {
		t.Fatalf("plan output is not clearly distinguished: %s", text.String())
	}
}

func findGraphNode(graph action.IntrospectionGraph, id string) *action.IntrospectionGraphNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == id {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func findGraphNodeKind(graph action.IntrospectionGraph, kind string) *action.IntrospectionGraphNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].Kind == kind {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func graphEdgesFrom(graph action.IntrospectionGraph, id string) []action.IntrospectionEdge {
	var edges []action.IntrospectionEdge
	for _, edge := range graph.Edges {
		if edge.From == id {
			edges = append(edges, edge)
		}
	}
	return edges
}

func compileExplainGraph(t *testing.T, cfg runner.Config, source string) action.IntrospectionGraph {
	t.Helper()
	var output bytes.Buffer
	if err := renderExplainActionGraph(context.Background(), &output, cfg, source, "structure.nflow", true, false); err != nil {
		t.Fatal(err)
	}
	var report ActionGraphReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode action graph: %v\n%s", err, output.String())
	}
	if report.Program == nil {
		t.Fatal("compiled program graph is missing")
	}
	for _, forbidden := range []string{
		"profile-secret", "metrics-secret", "ACTIVE", "running-secret", "other-secret",
		"db node is down", "recovered-secret", "fallback-secret", "pool-secret",
	} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("action graph leaked argument value %q", forbidden)
		}
	}
	return *report.Program
}
