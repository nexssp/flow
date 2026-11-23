package selfbench

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// RenderActionTree writes action spans in parent-child order. Inclusive wall
// time includes descendants; self wall time subtracts the union of direct child
// intervals and is not CPU time.
func RenderActionTree(w io.Writer, actions []ActionProfile) error {
	var out strings.Builder
	fmt.Fprintln(&out, "ACTION TREE  (inclusive/self are wall time; self subtracts direct-child span union, not CPU time)")
	if len(actions) == 0 {
		fmt.Fprintln(&out, "  no action spans recorded")
		_, err := io.WriteString(w, out.String())
		return err
	}

	children := make(map[string][]*ActionProfile, len(actions))
	for index := range actions {
		action := &actions[index]
		children[action.Parent] = append(children[action.Parent], action)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			if children[parent][i].TotalMs == children[parent][j].TotalMs {
				return children[parent][i].Path < children[parent][j].Path
			}
			return children[parent][i].TotalMs > children[parent][j].TotalMs
		})
	}

	var render func(string, int)
	render = func(parent string, depth int) {
		for _, action := range children[parent] {
			indent := strings.Repeat("  ", depth+1)
			fmt.Fprintf(&out, "%s%-14s %8.3f ms incl · %8.3f ms self · %d calls · %s",
				indent, graphBar(action.TotalMs, maxActionTotal(actions)), action.TotalMs, action.SelfMs, action.Calls, action.Action)
			if action.Errors > 0 {
				fmt.Fprintf(&out, " · %d errors", action.Errors)
			}
			if action.Retries > 0 {
				fmt.Fprintf(&out, " · %d retries", action.Retries)
			}
			fmt.Fprintln(&out)
			render(action.Path, depth+1)
		}
	}
	render("", 0)
	_, err := io.WriteString(w, out.String())
	return err
}

func maxActionTotal(actions []ActionProfile) float64 {
	var maximum float64
	for index := range actions {
		action := &actions[index]
		if action.TotalMs > maximum {
			maximum = action.TotalMs
		}
	}
	return maximum
}

// RenderCPUProfileSummary writes the saved pprof path and readable top
// functions. It is suitable for stderr so command output and JSON remain clean.
func RenderCPUProfileSummary(w io.Writer, summary CPUProfileSummary) error {
	var out strings.Builder
	fmt.Fprintf(&out, "CPU PROFILE  %s\n", summary.Path)
	if summary.TopFunctions != "" {
		fmt.Fprintln(&out, "TOP CPU FUNCTIONS  (go tool pprof -top)")
		fmt.Fprintln(&out, summary.TopFunctions)
	}
	if summary.AnalysisError != "" {
		fmt.Fprintf(&out, "CPU profile analysis: %s\n", summary.AnalysisError)
	}
	_, err := io.WriteString(w, out.String())
	return err
}
