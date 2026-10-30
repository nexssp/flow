package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func mermaidExampleFixtureSource(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", "mermaid_examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

func mermaidParallelFixtureSource(t *testing.T) string {
	return mermaidExampleFixtureSource(t, "parallel.nflow")
}

func TestParseExplainOptionsMermaidTheme(t *testing.T) {
	options, err := parseExplainOptions([]string{"--plan", "--mermaid", "--mermaid-theme", "dark", "api.nflow"})
	if err != nil {
		t.Fatal(err)
	}
	if options.path != "api.nflow" || !options.plan || !options.mermaid || options.mermaidTheme != actionPlanMermaidThemeDark {
		t.Fatalf("parsed options = %#v", options)
	}

	inline, err := parseExplainOptions([]string{"--plan", "--mermaid", "--mermaid-theme=light", "api.nflow"})
	if err != nil {
		t.Fatal(err)
	}
	if inline.mermaidTheme != actionPlanMermaidThemeLight {
		t.Fatalf("inline theme = %q, want light", inline.mermaidTheme)
	}

	defaultTheme, err := parseExplainOptions([]string{"--plan", "--mermaid", "api.nflow"})
	if err != nil {
		t.Fatal(err)
	}
	if defaultTheme.mermaidTheme != "" {
		t.Fatalf("unspecified theme = %q, want default selection at render time", defaultTheme.mermaidTheme)
	}

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "requires plan", args: []string{"api.nflow", "--mermaid"}, want: "--mermaid requires --plan"},
		{name: "mermaid requires plan with theme", args: []string{"api.nflow", "--mermaid", "--mermaid-theme", "dark"}, want: "--mermaid requires --plan"},
		{name: "theme requires mermaid", args: []string{"api.nflow", "--plan", "--mermaid-theme", "dark"}, want: "--mermaid-theme requires --mermaid"},
		{name: "invalid theme", args: []string{"api.nflow", "--plan", "--mermaid", "--mermaid-theme", "neon"}, want: "invalid --mermaid-theme"},
		{name: "missing theme value", args: []string{"api.nflow", "--mermaid-theme"}, want: "--mermaid-theme requires a value"},
		{name: "mutually exclusive with dot", args: []string{"api.nflow", "--plan", "--mermaid", "--dot"}, want: "mutually exclusive"},
		{name: "mutually exclusive with json", args: []string{"api.nflow", "--plan", "--mermaid", "--json"}, want: "mutually exclusive"},
		{name: "not an actions format", args: []string{"api.nflow", "--actions", "--mermaid"}, want: "requires --plan"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseExplainOptions(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseExplainOptions(%q) error = %v, want substring %q", test.args, err, test.want)
			}
		})
	}
}

func TestExplainActionPlanMermaidParallelBranchesThemesAndDeterminism(t *testing.T) {
	cfg := explainConfig(t)
	source := mermaidParallelFixtureSource(t)

	render := func(theme string) string {
		t.Helper()
		var output bytes.Buffer
		if err := renderExplainActionPlanWithFormatAndTheme(context.Background(), &output, cfg, source, "parallel.nflow", actionPlanOutputMermaid, theme); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}

	light := render(actionPlanMermaidThemeLight)
	if again := render(actionPlanMermaidThemeLight); again != light {
		t.Fatalf("light Mermaid output is not deterministic:\nfirst:\n%s\nsecond:\n%s", light, again)
	}
	if defaultLight := render(""); defaultLight != light {
		t.Fatalf("default Mermaid theme differs from explicit light theme:\nlight:\n%s\ndefault:\n%s", light, defaultLight)
	}
	dark := render(actionPlanMermaidThemeDark)
	if again := render(actionPlanMermaidThemeDark); again != dark {
		t.Fatalf("dark Mermaid output is not deterministic:\nfirst:\n%s\nsecond:\n%s", dark, again)
	}

	for _, want := range []string{
		"parallel fork",
		"join",
		"branch 1",
		"branch 2",
		"result:branch 1",
		"result:branch 2",
		"reads: ticket_id, customer_name, email",
		"outputs: ticket_id, contact_name, email",
		"reads: ticket_id, queue, order_id",
		"outputs: ticket_id, queue, order_id",
		"parallelFork",
		"classDef join",
		"#F8FAFC",
		"#C47A00",
	} {
		if !strings.Contains(light, want) {
			t.Errorf("light Mermaid output missing %q:\n%s", want, light)
		}
	}
	for _, want := range []string{"branch 1", "branch 2", "reads: ticket_id, customer_name, email", "outputs: ticket_id, contact_name, email", "reads: ticket_id, queue, order_id", "outputs: ticket_id, queue, order_id", "#0B1220", "#60A5FA", "#FBBF24", "#34D399"} {
		if !strings.Contains(dark, want) {
			t.Errorf("dark Mermaid output missing %q:\n%s", want, dark)
		}
	}
	if strings.Contains(light, "emits:") || strings.Contains(dark, "emits:") {
		t.Fatal("projection fields must use outputs terminology")
	}
	if strings.Count(light, " -->|branch ") != 2 || strings.Count(light, " -->|result:branch ") != 2 {
		t.Fatalf("parallel graph does not show two fan-out and two join edges:\n%s", light)
	}
	for _, forbidden := range []string{"PAYLOAD-MUST-NOT-RENDER", "julia@example.test", "TKT-7712", "ORD-7712", "Julia", "value:"} {
		if strings.Contains(light, forbidden) || strings.Contains(dark, forbidden) {
			t.Errorf("Mermaid output leaked action argument %q", forbidden)
		}
	}
}

func TestExplainActionPlanMermaidExamples(t *testing.T) {
	cfg := explainConfig(t)
	cases := []struct {
		name         string
		mustContain  []string
		mustNotShow  []string
		minSubgraphs int
	}{
		{
			name:         "linear.nflow",
			mustContain:  []string{"reads: order_id", "outputs: order_id, customer_name, item_count, currency"},
			mustNotShow:  []string{"ORD-3107", "Julia", "PAYLOAD-MUST-NOT-RENDER"},
			minSubgraphs: 1,
		},
		{
			name:         "large_order_fulfillment.nflow",
			mustContain:  []string{"@pipeline normalize_order", "@pipeline plan_delivery", "parallel fork", "branch 1", "branch 2", "result:branch 2", "outputs:"},
			mustNotShow:  []string{"ORD-5600", "CUS-4410", "julia@example.test", "profile-test-7", "WH-03", "PAYLOAD-MUST-NOT-RENDER"},
			minSubgraphs: 8,
		},
	}
	for _, test := range cases {
		for _, theme := range []string{actionPlanMermaidThemeLight, actionPlanMermaidThemeDark} {
			t.Run(test.name+"/"+theme, func(t *testing.T) {
				source := mermaidExampleFixtureSource(t, test.name)
				var output bytes.Buffer
				if err := renderExplainActionPlanWithFormatAndTheme(context.Background(), &output, cfg, source, test.name, actionPlanOutputMermaid, theme); err != nil {
					t.Fatal(err)
				}
				got := output.String()
				if strings.Count(got, "subgraph p") < test.minSubgraphs {
					t.Errorf("diagram has fewer than %d expected pipeline subgraphs", test.minSubgraphs)
				}
				if test.name == "large_order_fulfillment.nflow" &&
					(strings.Count(got, "parallel fork") != 2 || strings.Count(got, " -->|branch ") != 4 || strings.Count(got, " -->|result:branch ") != 4) {
					t.Errorf("large diagram should show three parallel pipeline checks as nested binary forks")
				}
				for _, want := range test.mustContain {
					if !strings.Contains(got, want) {
						t.Errorf("%s Mermaid output missing %q", theme, want)
					}
				}
				for _, forbidden := range test.mustNotShow {
					if strings.Contains(got, forbidden) {
						t.Errorf("%s Mermaid output leaked payload value %q", theme, forbidden)
					}
				}
				if strings.Contains(got, "emits:") || strings.Contains(got, "returns:") {
					t.Errorf("%s Mermaid projection summary uses non-approved terminology", theme)
				}
				palette := "#F8FAFC"
				if theme == actionPlanMermaidThemeDark {
					palette = "#0B1220"
				}
				if !strings.Contains(got, palette) {
					t.Errorf("%s Mermaid output missing theme background %s", theme, palette)
				}
			})
		}
	}
}

func TestActionPlanMermaidProjectionFieldsWrapWithoutDroppingFields(t *testing.T) {
	node := &ActionPlanNode{
		Kind:              "action",
		Name:              "projection.project",
		projectionReads:   []string{"customer.id", "customer.name", "customer.email", "order.id", "payment.profile"},
		projectionOutputs: []string{"customer_id", "customer_name", "contact_email", "order_id", "payment_profile"},
	}
	want := "projection\nreads: customer.id, customer.name, customer.email, order.id\n  payment.profile\noutputs: customer_id, customer_name, contact_email, order_id\n  payment_profile"
	if got := actionPlanMermaidNodeLabel(node); got != want {
		t.Fatalf("wrapped Mermaid projection label = %q, want %q", got, want)
	}
}

func TestExplainActionPlanMermaidOmitsComputedProjectionSummariesAndKeepsOtherFormatsUnchanged(t *testing.T) {
	cfg := explainConfig(t)
	computed := `{ name: .name, status: "PRIVATE-COMPUTED-VALUE" }`
	var output bytes.Buffer
	if err := renderExplainActionPlanWithFormatAndTheme(context.Background(), &output, cfg, computed, "computed-projection.nflow", actionPlanOutputMermaid, actionPlanMermaidThemeLight); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "reads:") || strings.Contains(output.String(), "outputs:") {
		t.Fatalf("computed projection received an unsupported field summary:\n%s", output.String())
	}
	if strings.Contains(output.String(), "PRIVATE-COMPUTED-VALUE") {
		t.Fatalf("computed projection value leaked:\n%s", output.String())
	}

	source := mermaidParallelFixtureSource(t)
	for _, format := range []actionPlanOutputFormat{actionPlanOutputJSON, actionPlanOutputDOT} {
		output.Reset()
		if err := renderExplainActionPlanWithFormat(context.Background(), &output, cfg, source, "parallel.nflow", format); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "reads:") || strings.Contains(output.String(), "outputs:") || strings.Contains(output.String(), "#F8FAFC") {
			t.Errorf("projection summary/theme changed non-Mermaid format %d:\n%s", format, output.String())
		}
		if strings.Contains(output.String(), "PAYLOAD-MUST-NOT-RENDER") {
			t.Errorf("non-Mermaid format %d leaked an action argument", format)
		}
	}
}

func TestStaticProjectionFieldSummaryRequiresDirectFieldReferences(t *testing.T) {
	summary, ok := staticProjectionFieldSummary(`{ name: .name, email: .customer.email, }`)
	if !ok {
		t.Fatal("simple direct-field projection did not produce a static summary")
	}
	if strings.Join(summary.reads, ",") != "name,customer.email" || strings.Join(summary.outputs, ",") != "name,email" {
		t.Fatalf("summary = %#v, want reads name/customer.email and outputs name/email", summary)
	}
	bodySummary, ok := staticProjectionFieldSummary(`name: .name, email: .customer.email,`)
	if !ok || strings.Join(bodySummary.reads, ",") != "name,customer.email" || strings.Join(bodySummary.outputs, ",") != "name,email" {
		t.Fatalf("brace-free projection summary = %#v, ok %t", bodySummary, ok)
	}
	for _, raw := range []string{
		`{ name: .name, status: "ready" }`,
		`{ name: upper(.name) }`,
		`{ .name: .name }`,
		`{ name: .name, name: .email }`,
		`{ name: .customer[0] }`,
	} {
		if _, ok := staticProjectionFieldSummary(raw); ok {
			t.Errorf("unsupported projection %q received a static summary", raw)
		}
	}
}

func TestWriteActionPlanMermaidEscapesLabelsAndOmitsLiteralNames(t *testing.T) {
	report := ActionPlanReport{
		Path: "private-source.nflow",
		TopLevel: &ActionPlanGraph{
			Nodes: []ActionPlanNode{
				{ID: "n1", Kind: "action", Name: `safe "name" [x] | <tag>&#`},
				{ID: "n2", Kind: "action", Name: "literal(payload-secret)", Line: 12},
			},
			Edges: []compiler.ActionGraphEdge{{From: "n1", To: "n2", Kind: `route|"quoted"`}},
		},
	}
	var output bytes.Buffer
	if err := writeActionPlanMermaid(&output, report); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`safe #quot;name#quot; #91;x#93; #124; #lt;tag#gt;#amp;#35;`,
		`|route#124;#quot;quoted#quot;|`,
		`action<br/>line 12`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("escaped Mermaid output missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"payload-secret", "private-source.nflow", `safe "name"`, "<tag>", `route|"quoted"`} {
		if strings.Contains(got, forbidden) {
			t.Errorf("Mermaid output contains unescaped or private text %q:\n%s", forbidden, got)
		}
	}
}
