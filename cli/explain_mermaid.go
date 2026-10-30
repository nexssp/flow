package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/nexssp/flow/compiler"
)

const (
	actionPlanMermaidThemeLight    = "light"
	actionPlanMermaidThemeDark     = "dark"
	actionPlanMermaidFieldsPerLine = 4
)

type mermaidPalette struct {
	background     string
	text           string
	line           string
	clusterFill    string
	clusterStroke  string
	edgeLabel      string
	actionFill     string
	actionStroke   string
	actionText     string
	parallelFill   string
	parallelStroke string
	parallelText   string
	joinFill       string
	joinStroke     string
	joinText       string
	decisionFill   string
	decisionStroke string
	decisionText   string
	fontFamily     string
	fontSize       string
}

func writeActionPlanMermaid(w io.Writer, report ActionPlanReport) error {
	return writeActionPlanMermaidWithTheme(w, report, actionPlanMermaidThemeLight)
}

func writeActionPlanMermaidWithTheme(w io.Writer, report ActionPlanReport, theme string) error {
	palette, err := actionPlanMermaidPaletteFor(theme)
	if err != nil {
		return err
	}

	initConfig := map[string]any{
		"theme": "base",
		"themeVariables": map[string]string{
			"background":           palette.background,
			"textColor":            palette.text,
			"primaryColor":         palette.actionFill,
			"primaryBorderColor":   palette.actionStroke,
			"primaryTextColor":     palette.actionText,
			"secondaryColor":       palette.parallelFill,
			"secondaryBorderColor": palette.parallelStroke,
			"secondaryTextColor":   palette.parallelText,
			"tertiaryColor":        palette.joinFill,
			"tertiaryBorderColor":  palette.joinStroke,
			"tertiaryTextColor":    palette.joinText,
			"lineColor":            palette.line,
			"clusterBkg":           palette.clusterFill,
			"clusterBorder":        palette.clusterStroke,
			"edgeLabelBackground":  palette.edgeLabel,
			"fontFamily":           palette.fontFamily,
			"fontSize":             palette.fontSize,
		},
		"themeCSS": fmt.Sprintf(".labelBkg, .edgeLabel, .edgeLabel p { background-color: %s !important; color: %s !important; }", palette.edgeLabel, palette.text),
		"flowchart": map[string]any{
			"htmlLabels":  true,
			"curve":       "basis",
			"nodeSpacing": 55,
			"rankSpacing": 70,
			"padding":     12,
		},
	}
	initJSON, err := json.Marshal(initConfig)
	if err != nil {
		return fmt.Errorf("encode Mermaid theme: %w", err)
	}
	if _, err := io.WriteString(w, "%%{init: "+string(initJSON)+"}%%\nflowchart LR\n"); err != nil {
		return err
	}

	var classes []mermaidClassAssignment
	cluster := 0
	if report.TopLevel != nil {
		cluster++
		group, err := writeActionPlanMermaidSubgraph(w, cluster, "top-level", report.TopLevel, palette)
		if err != nil {
			return err
		}
		classes = append(classes, group...)
	}
	for _, pipeline := range report.Pipelines {
		cluster++
		group, err := writeActionPlanMermaidSubgraph(w, cluster, "@pipeline "+pipeline.Name, &pipeline.Graph, palette)
		if err != nil {
			return err
		}
		classes = append(classes, group...)
	}

	if _, err := fmt.Fprintf(w, "  classDef action fill:%s,stroke:%s,color:%s,stroke-width:1.5px\n", palette.actionFill, palette.actionStroke, palette.actionText); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  classDef parallelFork fill:%s,stroke:%s,color:%s,stroke-width:2.5px\n", palette.parallelFill, palette.parallelStroke, palette.parallelText); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  classDef join fill:%s,stroke:%s,color:%s,stroke-width:2px\n", palette.joinFill, palette.joinStroke, palette.joinText); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  classDef decision fill:%s,stroke:%s,color:%s,stroke-width:2px\n", palette.decisionFill, palette.decisionStroke, palette.decisionText); err != nil {
		return err
	}
	for _, class := range classes {
		if _, err := fmt.Fprintf(w, "  class %s %s\n", class.nodeID, class.name); err != nil {
			return err
		}
	}
	return nil
}

func actionPlanMermaidPaletteFor(theme string) (mermaidPalette, error) {
	fontFamily := "Inter, ui-sans-serif, system-ui, sans-serif"
	switch theme {
	case "", actionPlanMermaidThemeLight:
		return mermaidPalette{
			background:     "#F8FAFC",
			text:           "#132238",
			line:           "#52647A",
			clusterFill:    "#F1F5F9",
			clusterStroke:  "#C5D1DF",
			edgeLabel:      "#FFFFFF",
			actionFill:     "#E8F1FF",
			actionStroke:   "#3568B0",
			actionText:     "#102A43",
			parallelFill:   "#FFF1CC",
			parallelStroke: "#C47A00",
			parallelText:   "#5C3B00",
			joinFill:       "#D9F5E8",
			joinStroke:     "#138A62",
			joinText:       "#064E3B",
			decisionFill:   "#F3E8FF",
			decisionStroke: "#8B5CF6",
			decisionText:   "#4C1D95",
			fontFamily:     fontFamily,
			fontSize:       "16px",
		}, nil
	case actionPlanMermaidThemeDark:
		return mermaidPalette{
			background:     "#0B1220",
			text:           "#E7EEF9",
			line:           "#B7C7E2",
			clusterFill:    "#121D30",
			clusterStroke:  "#334761",
			edgeLabel:      "#142033",
			actionFill:     "#183252",
			actionStroke:   "#60A5FA",
			actionText:     "#E6F1FF",
			parallelFill:   "#3D2D11",
			parallelStroke: "#FBBF24",
			parallelText:   "#FFF1C7",
			joinFill:       "#103A32",
			joinStroke:     "#34D399",
			joinText:       "#D1FAE5",
			decisionFill:   "#331C42",
			decisionStroke: "#C084FC",
			decisionText:   "#F3E8FF",
			fontFamily:     fontFamily,
			fontSize:       "16px",
		}, nil
	default:
		return mermaidPalette{}, fmt.Errorf("invalid Mermaid theme %q (want light or dark)", theme)
	}
}

type mermaidClassAssignment struct {
	nodeID string
	name   string
}

func writeActionPlanMermaidSubgraph(w io.Writer, cluster int, label string, graph *ActionPlanGraph, palette mermaidPalette) ([]mermaidClassAssignment, error) {
	prefix := "p" + strconv.Itoa(cluster) + "_"
	if _, err := fmt.Fprintf(w, "  subgraph %s[\"%s\"]\n", prefix, escapeMermaidLabel(label)); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, "    direction LR\n"); err != nil {
		return nil, err
	}

	nodeIDs := make(map[string]string, len(graph.Nodes))
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		nodeIDs[node.ID] = prefix + "n" + strconv.Itoa(i+1)
	}
	classes := make([]mermaidClassAssignment, 0, len(graph.Nodes))
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		nodeID := prefix + "n" + strconv.Itoa(i+1)
		label := escapeMermaidLabel(actionPlanMermaidNodeLabel(node))
		shape := `["` + label + `"]`
		switch {
		case strings.HasSuffix(node.Kind, ".fork"), node.Kind == actionPlanNodeKindConditional, node.Kind == actionPlanNodeKindFallback, node.Kind == actionPlanNodeKindMatch, node.Kind == actionPlanNodeKindErrorGuard, node.Kind == actionPlanNodeKindLoop:
			shape = `{"` + label + `"}`
		case node.Kind == actionPlanNodeKindMerge, node.Kind == actionPlanNodeKindLoopExit:
			shape = `(("` + label + `"))`
		}
		if _, err := fmt.Fprintf(w, "    %s%s\n", nodeID, shape); err != nil {
			return nil, err
		}
		classes = append(classes, mermaidClassAssignment{nodeID: nodeID, name: actionPlanMermaidClass(node.Kind)})
	}
	for _, edge := range graph.Edges {
		from, fromOK := nodeIDs[edge.From]
		to, toOK := nodeIDs[edge.To]
		if !fromOK || !toOK {
			return nil, fmt.Errorf("action plan edge %q -> %q references an unknown node", edge.From, edge.To)
		}
		if edge.Kind == "" {
			if _, err := fmt.Fprintf(w, "    %s --> %s\n", from, to); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "    %s -->|%s| %s\n", from, escapeMermaidLabel(edge.Kind), to); err != nil {
			return nil, err
		}
	}
	if _, err := io.WriteString(w, "  end\n"); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "  style %s fill:%s,stroke:%s,stroke-width:1.5px\n", prefix, palette.clusterFill, palette.clusterStroke); err != nil {
		return nil, err
	}
	return classes, nil
}

func actionPlanMermaidNodeLabel(node *ActionPlanNode) string {
	label := node.Kind
	switch {
	case node.Kind == "action" && node.Name == "projection.project":
		label = "projection"
	case strings.HasSuffix(node.Kind, ".fork"):
		label = strings.TrimSuffix(node.Kind, ".fork") + " fork"
	case node.Kind == actionPlanNodeKindMerge:
		label = "join"
	case node.Kind == actionPlanNodeKindLoopExit:
		label = "loop exit"
	case node.Kind == actionPlanNodeKindConditional, node.Kind == actionPlanNodeKindFallback, node.Kind == actionPlanNodeKindMatch, node.Kind == actionPlanNodeKindErrorGuard:
		label = "decision"
	}

	name := node.Name
	skipName := name == "projection.project" || strings.HasPrefix(name, "literal(") ||
		(strings.HasSuffix(node.Kind, ".fork") && name == strings.TrimSuffix(node.Kind, ".fork")) ||
		(node.Kind == actionPlanNodeKindMerge && strings.HasSuffix(name, ".result")) ||
		(node.Kind == actionPlanNodeKindConditional && name == actionPlanNodeKindConditional)
	if name != "" && !skipName {
		label += "\n" + name
	}
	if node.Line > 0 {
		label += "\nline " + strconv.Itoa(node.Line)
	}
	if len(node.projectionReads) > 0 {
		label += "\n" + formatActionPlanMermaidProjectionFields("reads", node.projectionReads)
	}
	if len(node.projectionOutputs) > 0 {
		label += "\n" + formatActionPlanMermaidProjectionFields("outputs", node.projectionOutputs)
	}
	return label
}

func formatActionPlanMermaidProjectionFields(label string, fields []string) string {
	lines := make([]string, 0, (len(fields)+actionPlanMermaidFieldsPerLine-1)/actionPlanMermaidFieldsPerLine)
	for start := 0; start < len(fields); start += actionPlanMermaidFieldsPerLine {
		end := min(start+actionPlanMermaidFieldsPerLine, len(fields))
		fieldLine := strings.Join(fields[start:end], ", ")
		if start == 0 {
			fieldLine = label + ": " + fieldLine
		} else {
			fieldLine = "  " + fieldLine
		}
		lines = append(lines, fieldLine)
	}
	return strings.Join(lines, "\n")
}

func actionPlanMermaidClass(kind string) string {
	switch {
	case strings.HasSuffix(kind, ".fork"):
		return "parallelFork"
	case kind == actionPlanNodeKindMerge || kind == actionPlanNodeKindLoopExit:
		return "join"
	case kind == actionPlanNodeKindConditional || kind == actionPlanNodeKindFallback || kind == actionPlanNodeKindMatch || kind == actionPlanNodeKindErrorGuard || kind == actionPlanNodeKindLoop:
		return "decision"
	default:
		return "action"
	}
}

// escapeMermaidLabel encodes Mermaid syntax characters as Mermaid entity
// references. Newlines become explicit HTML line breaks inside a label.
func escapeMermaidLabel(label string) string {
	var escaped strings.Builder
	for _, r := range label {
		switch r {
		case '\n':
			escaped.WriteString("<br/>")
		case '\r':
			// Drop carriage returns; line endings are represented by <br/>.
		case '"':
			escaped.WriteString("#quot;")
		case '&':
			escaped.WriteString("#amp;")
		case '<':
			escaped.WriteString("#lt;")
		case '>':
			escaped.WriteString("#gt;")
		case '|':
			escaped.WriteString("#124;")
		case '[':
			escaped.WriteString("#91;")
		case ']':
			escaped.WriteString("#93;")
		case '\\':
			escaped.WriteString("#92;")
		case '#':
			escaped.WriteString("#35;")
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&escaped, "#%d;", r)
			} else {
				escaped.WriteRune(r)
			}
		}
	}
	return escaped.String()
}

type actionPlanNodePosition struct {
	file   string
	line   int
	column int
}

type projectionFieldSummary struct {
	reads   []string
	outputs []string
}

func actionPlanProjectionFieldSummaries(expr compiler.Expr) map[actionPlanNodePosition]projectionFieldSummary {
	summaries := make(map[actionPlanNodePosition]projectionFieldSummary)
	var visit func(compiler.Expr)
	visit = func(current compiler.Expr) {
		if current == nil {
			return
		}
		if projection, ok := current.(*compiler.ProjectionExpr); ok {
			if summary, ok := staticProjectionFieldSummary(projection.Raw); ok {
				pos := projection.Pos
				summaries[actionPlanNodePosition{file: pos.File, line: pos.Line, column: pos.Col}] = summary
			}
			return
		}
		for _, child := range current.Children() {
			visit(child)
		}
	}
	visit(expr)
	return summaries
}

// staticProjectionFieldSummary reports only the deliberately small subset
// where each output map field is a direct reference such as `email: .customer.email`.
// The parser stores ProjectionExpr.Raw without braces; wrapped map text is also
// accepted for focused tests. Computed values, literals, quoted keys, and
// dynamic access are omitted.
func staticProjectionFieldSummary(raw string) (projectionFieldSummary, bool) {
	expression := strings.TrimSpace(raw)
	if strings.HasPrefix(expression, "{") || strings.HasSuffix(expression, "}") {
		if len(expression) < 2 || expression[0] != '{' || expression[len(expression)-1] != '}' {
			return projectionFieldSummary{}, false
		}
		expression = strings.TrimSpace(expression[1 : len(expression)-1])
	}
	body := expression
	if strings.ContainsAny(body, "{}") {
		return projectionFieldSummary{}, false
	}
	if body == "" {
		return projectionFieldSummary{}, false
	}

	parts := strings.Split(body, ",")
	summary := projectionFieldSummary{}
	seenOutputs := make(map[string]struct{}, len(parts))
	seenReads := make(map[string]struct{}, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" && i == len(parts)-1 {
			continue // a trailing comma is safe
		}
		key, value, found := strings.Cut(part, ":")
		key = strings.TrimSpace(key)
		if !found || !isSimpleProjectionIdentifier(key) {
			return projectionFieldSummary{}, false
		}
		if _, duplicate := seenOutputs[key]; duplicate {
			return projectionFieldSummary{}, false
		}
		path, ok := directProjectionFieldPath(value)
		if !ok {
			return projectionFieldSummary{}, false
		}
		seenOutputs[key] = struct{}{}
		summary.outputs = append(summary.outputs, key)
		if _, seen := seenReads[path]; !seen {
			seenReads[path] = struct{}{}
			summary.reads = append(summary.reads, path)
		}
	}
	if len(summary.outputs) == 0 {
		return projectionFieldSummary{}, false
	}
	return summary, true
}

func directProjectionFieldPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, ".") {
		return "", false
	}
	parts := strings.Split(value[1:], ".")
	if len(parts) == 0 {
		return "", false
	}
	for _, part := range parts {
		if !isSimpleProjectionIdentifier(part) {
			return "", false
		}
	}
	return strings.Join(parts, "."), true
}

func isSimpleProjectionIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		c := value[i]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		digit := c >= '0' && c <= '9'
		if i == 0 {
			if !letter && c != '_' {
				return false
			}
		} else if !letter && !digit && c != '_' {
			return false
		}
	}
	return true
}
