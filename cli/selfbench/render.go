package selfbench

import (
	"fmt"
	"io"
	"strings"

	"github.com/nexssp/flow/cli/internal/termstyle"
)

const bannerSubtitle = "compile/setup and first run are in-process, not process cold start; warmup excluded"

func renderReport(report Report, w io.Writer) error {
	style := termstyle.New(w)
	var out strings.Builder
	out.WriteString(style.Banner("SELF BENCH", bannerSubtitle))
	fmt.Fprintf(&out, "  %s %s · %s/%s · %d CPUs · GOMAXPROCS=%d\n\n",
		style.Bold("HOST"), report.GoVersion, report.OS, report.Arch, report.CPUs, report.GOMAXPROCS)

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

func reportLabel(style termstyle.Style, label string) string {
	return style.Bold(fmt.Sprintf("%-18s", label))
}
