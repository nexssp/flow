package selfbench

import (
	"fmt"
	"io"
	"strings"

	"github.com/nexssp/flow/cli/internal/termstyle"
)

const (
	bannerSubtitle = "in-process timing; allocations are Go heap bytes created, not live memory or RSS"
	graphBarWidth  = 14
)

func renderReport(report Report, w io.Writer) error {
	style := termstyle.New(w)
	var out strings.Builder
	out.WriteString(style.Banner("SELF BENCH", bannerSubtitle))
	fmt.Fprintf(&out, "  %s %s · %s/%s · %d CPUs · GOMAXPROCS=%d\n\n",
		style.Bold("HOST"), report.GoVersion, report.OS, report.Arch, report.CPUs, report.GOMAXPROCS)
	if report.Profiled {
		out.WriteString(style.Dim("  action profile enabled; profiling hooks are included in measured timings\n\n"))
	}

	passed, failed := 0, 0
	for i := range report.Results {
		result := &report.Results[i]
		status := style.Green("✓ passed")
		if result.Error != "" || result.Warm.Errors > 0 {
			failed++
			status = style.Red("✗ failed")
		} else {
			passed++
		}
		out.WriteString(style.Section(result.Name))
		fmt.Fprintf(&out, "  %s %s\n", reportLabel(style, "Status"), status)
		fmt.Fprintf(&out, "  %s %.3f ms\n", reportLabel(style, "Compilation"), result.CompileMs)
		fmt.Fprintf(&out, "  %s %.3f ms\n", reportLabel(style, "First run"), result.FirstRunMs)
		fmt.Fprintf(&out, "  %s %d runs\n", reportLabel(style, "Warmup"), result.Warmup)
		fmt.Fprintf(&out, "  %s %d samples\n", reportLabel(style, "Warm samples"), result.Warm.Iterations)
		fmt.Fprintf(&out, "  %s min %.3f · mean %.3f · max %.3f ms\n", reportLabel(style, "Latency"), result.Warm.MinMs, result.Warm.MeanMs, result.Warm.MaxMs)
		fmt.Fprintf(&out, "  %s p50 %.3f · p95 %.3f · p99 %.3f ms\n", reportLabel(style, "Distribution"), result.Warm.P50Ms, result.Warm.P95Ms, result.Warm.P99Ms)
		fmt.Fprintf(&out, "  %s %.1f requests/s\n", reportLabel(style, "Throughput"), result.Warm.RPS)
		fmt.Fprintf(&out, "  %s %d\n", reportLabel(style, "Sample errors"), result.Warm.Errors)
		renderPhaseGraph(&out, *result)
		if report.Profiled {
			renderActionGraph(&out, result.Actions)
		}
		if result.Error != "" {
			fmt.Fprintf(&out, "  %s %s\n", reportLabel(style, "Error"), style.Red(result.Error))
		}
	}

	failSummary := style.Dim("0 failed")
	if failed > 0 {
		failSummary = style.Red(fmt.Sprintf("✗ %d failed", failed))
	}
	fmt.Fprintf(&out, "\n  %s  %s  %s\n\n",
		style.Green(fmt.Sprintf("✓ %d passed", passed)), failSummary,
		style.Bold(fmt.Sprintf("(%d total)", len(report.Results))))
	_, err := io.WriteString(w, out.String())
	return err
}

func renderPhaseGraph(out *strings.Builder, result Result) {
	rows := []phaseGraphRow{
		{
			name: "compile", milliseconds: result.CompileMs, timeMeasured: true,
			allocBytes: float64(result.CompileAllocation.Bytes), allocCount: float64(result.CompileAllocation.Allocations),
		},
		{
			name: "first run", milliseconds: result.FirstRunMs, timeMeasured: true,
			allocBytes: float64(result.FirstRunAllocation.Bytes), allocCount: float64(result.FirstRunAllocation.Allocations),
		},
		{
			name: "warmup / run", allocBytes: result.WarmupAllocation.BytesPerRun, allocCount: result.WarmupAllocation.AllocationsPerRun,
			allocationRuns: result.WarmupAllocation.Runs,
		},
		{
			name: "warm / run", milliseconds: result.Warm.MeanMs, timeMeasured: result.Warm.Iterations > 0,
			allocBytes: result.WarmAllocation.BytesPerRun, allocCount: result.WarmAllocation.AllocationsPerRun,
			allocationRuns: result.WarmAllocation.Runs,
		},
	}
	maxTime, maxBytes := 0.0, 0.0
	for _, row := range rows {
		if row.timeMeasured && row.milliseconds > maxTime {
			maxTime = row.milliseconds
		}
		if row.allocBytes > maxBytes {
			maxBytes = row.allocBytes
		}
	}
	fmt.Fprintln(out, "  PHASE GRAPH  (T = time; A = allocated Go heap bytes; bars scale independently)")
	for _, row := range rows {
		timeBar, timeValue := "—", "not timed"
		if row.timeMeasured {
			timeBar = graphBar(row.milliseconds, maxTime)
			timeValue = fmt.Sprintf("%.3f ms", row.milliseconds)
		}
		allocationBar := graphBar(row.allocBytes, maxBytes)
		allocationValue := formatBytes(row.allocBytes) + " · " + formatAllocationCount(row.allocCount)
		if row.allocationRuns > 0 {
			allocationValue += " / run"
		} else if row.name == "warmup / run" || row.name == "warm / run" {
			allocationValue = "not sampled"
		}
		fmt.Fprintf(out, "    %-12s T %-14s %9s   A %-14s %s\n",
			row.name, timeBar, timeValue, allocationBar, allocationValue)
	}
}

type phaseGraphRow struct {
	name           string
	milliseconds   float64
	timeMeasured   bool
	allocBytes     float64
	allocCount     float64
	allocationRuns int
}

func renderActionGraph(out *strings.Builder, actions []ActionProfile) {
	fmt.Fprintln(out, "  ACTION GRAPH  (total inclusive action time across measured warm runs; nested actions can overlap)")
	maxTotal := 0.0
	for _, action := range actions {
		if action.TotalMs > maxTotal {
			maxTotal = action.TotalMs
		}
	}
	if len(actions) == 0 {
		fmt.Fprintln(out, "    no action lifecycle events recorded")
		return
	}
	for _, action := range actions {
		fmt.Fprintf(out, "    %-14s %-35s %8.3f ms total · %8.3f ms/call · %d calls",
			graphBar(action.TotalMs, maxTotal), action.Action, action.TotalMs, action.MeanMs, action.Calls)
		if action.Errors > 0 {
			fmt.Fprintf(out, " · %d errors", action.Errors)
		}
		fmt.Fprintln(out)
	}
}

func graphBar(value, maximum float64) string {
	if value <= 0 || maximum <= 0 {
		return strings.Repeat(" ", graphBarWidth)
	}
	length := max(int(value/maximum*graphBarWidth), 1)
	length = min(length, graphBarWidth)
	return strings.Repeat("#", length) + strings.Repeat(" ", graphBarWidth-length)
}

func formatBytes(bytes float64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%.0f B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", bytes/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MiB", bytes/(1024*1024))
	}
	return fmt.Sprintf("%.1f GiB", bytes/(1024*1024*1024))
}

func formatAllocationCount(allocations float64) string {
	return fmt.Sprintf("%.0f allocs", allocations)
}

func reportLabel(style termstyle.Style, label string) string {
	return style.Bold(fmt.Sprintf("%-18s", label))
}
