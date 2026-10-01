package selftest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nexssp/flow/core"
	flowrunner "github.com/nexssp/flow/runner"
)

// Renderer writes the live checklist. When its writer is a TTY it
// overwrites each feature line in place as the result arrives; when
// redirected, it prints plain lines suitable for CI logs.
type Renderer struct {
	w        io.Writer
	tty      bool
	color    bool
	verbose  bool
	mu       sync.Mutex
	passed   int
	failed   int
	skipped  int
	total    int
	perSect  map[string]int
	sectSize map[string]int
}

func newRenderer(opts Options) *Renderer {
	return &Renderer{
		w:        opts.Out,
		tty:      isTerminal(opts.Out),
		color:    !opts.NoColor,
		verbose:  opts.Verbose,
		perSect:  map[string]int{},
		sectSize: map[string]int{},
	}
}

// ── Duration formatting ──────────────────────────────────────────────

func formatElapsed(d time.Duration) string {
	switch {
	case d <= 0:
		return "0s"
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000.0)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// ── ANSI helpers ─────────────────────────────────────────────────────

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)

func (r *Renderer) paint(code, s string) string {
	if !r.color {
		return s
	}
	return code + s + ansiReset
}

func (r *Renderer) dim(s string) string    { return r.paint(ansiDim, s) }
func (r *Renderer) bold(s string) string   { return r.paint(ansiBold, s) }
func (r *Renderer) red(s string) string    { return r.paint(ansiRed, s) }
func (r *Renderer) green(s string) string  { return r.paint(ansiGreen, s) }
func (r *Renderer) yellow(s string) string { return r.paint(ansiYellow, s) }
func (r *Renderer) cyan(s string) string   { return r.paint(ansiCyan, s) }
func (r *Renderer) mag(s string) string    { return r.paint(ansiMagenta, s) }

// ── Banner ───────────────────────────────────────────────────────────

func (r *Renderer) Banner(sections []core.SelfTestSection) {
	for _, s := range sections {
		r.total += len(s.Features)
		r.sectSize[s.Name] = len(s.Features)
	}

	line := strings.Repeat("─", 70)
	host := DetectHost()

	r.printf("\n%s\n", r.mag(line))
	r.printf("%s\n", r.bold("  NEXSS FLOW · SELF TEST"))
	r.printf("%s\n", r.dim("  "+host.FormatBanner()))
	r.printf("%s\n\n", r.mag(line))
}

// ── Section ──────────────────────────────────────────────────────────

func (r *Renderer) SectionStart(s core.SelfTestSection) {
	r.printf("\n%s %s  %s\n\n",
		r.cyan("▸"),
		r.bold(strings.ToUpper(s.Name)),
		r.dim(fmt.Sprintf("%d features", len(s.Features))),
	)
}

func (r *Renderer) SectionDone(s core.SelfTestSection, all []Result, elapsed time.Duration) {
	passed, failed, skipped := 0, 0, 0
	for i := range all {
		res := all[i]
		if !featureInSection(res.SelfTestFeature, s) {
			continue
		}
		switch res.Status {
		case StatusPass:
			passed++
		case StatusFail:
			failed++
		case StatusSkip:
			skipped++
		}
	}

	badge := r.green(fmt.Sprintf("✓ %d", passed))
	if failed > 0 {
		badge = r.red(fmt.Sprintf("✗ %d", failed))
	}
	if skipped > 0 {
		badge += " " + r.dim(fmt.Sprintf("⊘ %d", skipped))
	}
	r.printf("\n  %s  %s  %s\n",
		r.dim("└─"),
		badge,
		r.dim(formatElapsed(elapsed)),
	)
}

func featureInSection(f core.SelfTestFeature, s core.SelfTestSection) bool {
	for _, sf := range s.Features {
		if sf.Name == f.Name {
			return true
		}
	}
	return false
}

// ── Feature ──────────────────────────────────────────────────────────

func (r *Renderer) FeatureStart(_ core.SelfTestFeature) {
	// Live spinner was removed; kept as a no-op hook so call sites stay
	// symmetrical.
}

// FeatureDone prints one feature line: elapsed time, allocation
// counts split by phase, and — when the feature lives in an embedded
// *.nflow fixture — the path of that file. The path is deliberately
// shown last so the aligned columns stay readable when only some
// features are file-backed.
func (r *Renderer) FeatureDone(res Result) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch res.Status {
	case StatusPass:
		r.passed++
	case StatusFail:
		r.failed++
	case StatusSkip:
		r.skipped++
	}

	icon, color := "✓", ansiGreen
	switch res.Status {
	case StatusPass:
	case StatusFail:
		icon, color = "✗", ansiRed
	case StatusSkip:
		icon, color = "⊘", ansiDim
	}

	timeColumn := "        "
	allocColumn := "            "
	sourceColumn := ""
	if res.Status != StatusSkip {
		timeColumn = fmt.Sprintf("%-8s", formatElapsed(res.Elapsed))
		allocColumn = fmt.Sprintf("%-12s", formatCount(res.CompileAllocs)+"c "+formatCount(res.RunAllocs)+"r")
		if res.Source != "" {
			sourceColumn = "  " + res.Source
		}
	}

	r.printf("  %s   %-38s %s %s%s\n",
		r.paint(color, icon),
		res.Name,
		r.dim(timeColumn),
		r.dim(allocColumn),
		r.dim(sourceColumn),
	)

	switch res.Status {
	case StatusPass:
	case StatusFail:
		if res.Err != nil {
			r.printf("       %s %s\n", r.dim("└─"), r.red(res.Err.Error()))
		}
		if r.verbose {
			for line := range strings.SplitSeq(res.DSL, "\n") {
				r.printf("       %s %s\n", r.dim("│"), r.dim(line))
			}
		}
	case StatusSkip:
		if res.SkipReason != "" {
			r.printf("       %s %s\n", r.dim("└─"), r.dim(res.SkipReason))
		}
	}
}

// ── Summary ──────────────────────────────────────────────────────────

func (r *Renderer) Summary(results []Result, elapsed time.Duration) {
	line := strings.Repeat("─", 70)

	var (
		passed        int
		failed        int
		skipped       int
		compileAllocs uint64
		runAllocs     uint64
		compileBytes  uint64
		runBytes      uint64
		failures      []Result
	)
	for i := range results {
		res := results[i]
		switch res.Status {
		case StatusPass:
			passed++
			compileAllocs += res.CompileAllocs
			runAllocs += res.RunAllocs
			compileBytes += res.CompileAllocBytes
			runBytes += res.RunAllocBytes
		case StatusFail:
			failed++
			compileAllocs += res.CompileAllocs
			runAllocs += res.RunAllocs
			compileBytes += res.CompileAllocBytes
			runBytes += res.RunAllocBytes
			failures = append(failures, res)
		case StatusSkip:
			skipped++
		}
	}

	r.printf("\n%s\n", r.mag(line))

	r.printf("  %-14s %s\n", r.bold("PASSED"), r.green(strconv.Itoa(passed)))
	if failed > 0 {
		r.printf("  %-14s %s\n", r.bold("FAILED"), r.red(strconv.Itoa(failed)))
	} else {
		r.printf("  %-14s %s\n", r.bold("FAILED"), r.dim("0"))
	}
	r.printf("  %-14s %s\n", r.bold("SKIPPED"), r.dim(strconv.Itoa(skipped)))
	r.printf("  %-14s %s\n", r.bold("TOTAL"), strconv.Itoa(passed+failed+skipped))
	r.printf("  %-14s %s\n", r.bold("TIME"), formatElapsed(elapsed))

	if compileAllocs+runAllocs > 0 {
		r.printf("  %-14s %s allocs · %s\n",
			r.bold("ALLOCATIONS"),
			formatCount(compileAllocs+runAllocs),
			formatBytes(compileBytes+runBytes),
		)
		r.printf("  %-14s %sc compile · %sr run\n",
			r.dim("  split"),
			r.dim(formatCount(compileAllocs)),
			r.dim(formatCount(runAllocs)),
		)
	}

	if len(failures) > 0 {
		r.printFailures(failures)
	}

	r.printf("\n%s\n", r.mag(line))

	if failed == 0 {
		r.printf("  %s\n\n", r.green(r.bold("✓ all selected features passed")))
	} else {
		r.printf("  %s\n\n", r.red(r.bold(fmt.Sprintf("✗ %d feature(s) failed — see list above", failed))))
	}
	r.printf("%s\n", r.mag(line))
}

func (r *Renderer) printFailures(failures []Result) {
	bar := strings.Repeat("─", 34)
	r.printf("\n  %s %s\n\n",
		r.red(bar),
		r.red(r.bold("FAILED FEATURES")),
	)

	for i := range failures {
		res := failures[i]
		section := res.Section
		if section == "" {
			section = "?"
		}

		r.printf("  %s  %s\n",
			r.red("✗"),
			r.bold(res.Name),
		)
		r.printf("       %s %s\n",
			r.dim("├─ section:"),
			r.dim(section),
		)
		if res.Source != "" {
			r.printf("       %s %s\n",
				r.dim("├─ source: "),
				r.dim(res.Source),
			)
		}
		if res.Err != nil {
			r.printf("       %s %s\n",
				r.dim("└─ error:  "),
				r.red(res.Err.Error()),
			)
		}
		r.printf("\n")
	}
}

// ── Score summary ────────────────────────────────────────────────────

func (r *Renderer) ScoreSummary(s Score, host HostInfo) {
	line := strings.Repeat("─", 70)
	r.printf("\n%s\n", r.mag(line))

	r.printf("  %-14s %s\n", r.bold("HOST"), r.dim(host.FormatBanner()))

	r.printf("  %-14s %s  %s\n",
		r.bold("SCORE"),
		r.green(r.bold(fmt.Sprintf("%.1f / 100", s.Value()))),
		r.cyan(s.Grade()),
	)
	r.printf("  %-14s %s\n", r.bold("FINGERPRINT"), r.dim(s.Fingerprint))

	if s.TotalAllocs > 0 {
		r.printf("  %-14s %s allocs · %s\n",
			r.bold("ALLOCATIONS"),
			formatCount(s.TotalAllocs),
			formatBytes(s.TotalAllocBytes),
		)
		r.printf("  %-14s %sc compile · %sr run\n",
			r.dim("  split"),
			r.dim(formatCount(s.TotalCompileAllocs)),
			r.dim(formatCount(s.TotalRunAllocs)),
		)
	}

	switch {
	case !s.BaselinePresent:
		r.printf("  %-14s %s\n", r.bold("BASELINE"), r.dim("none (run with --save-baseline to set)"))
	case s.BaselineMatched:
		r.printf("  %-14s %s %s\n",
			r.bold("BASELINE"),
			r.green("✓ match"),
			r.dim("("+s.BaselineFingerprint+")"),
		)
	default:
		r.printf("  %-14s %s %s\n",
			r.bold("BASELINE"),
			r.yellow("⚠ differs"),
			r.dim("(was "+s.BaselineFingerprint+", now "+s.Fingerprint+")"),
		)
	}
	r.printf("%s\n", r.mag(line))
}

// TopMetrics prints the slowest features and the biggest allocators.
// The allocator list shows the compile and run split so a reader can
// tell which phase dominated for that feature.
func (r *Renderer) TopMetrics(s Score) {
	if len(s.Slowest) == 0 && len(s.TopAllocators) == 0 {
		return
	}
	line := strings.Repeat("─", 70)
	r.printf("\n%s\n", r.mag(line))

	if len(s.Slowest) > 0 {
		r.printf("  %s\n", r.bold("SLOWEST FEATURES"))
		for i, f := range s.Slowest {
			r.printf("    %d. %-42s %s\n",
				i+1,
				f.Name,
				r.dim(formatElapsed(f.Elapsed)),
			)
		}
	}

	if len(s.TopAllocators) > 0 {
		r.printf("\n  %s\n", r.bold("TOP ALLOCATORS"))
		for i, f := range s.TopAllocators {
			r.printf("    %d. %-42s %sc compile · %sr run\n",
				i+1,
				f.Name,
				r.dim(formatCount(f.CompileAllocs)),
				r.dim(formatCount(f.RunAllocs)),
			)
		}
	}
	r.printf("%s\n", r.mag(line))
}

// ── Helpers ──────────────────────────────────────────────────────────

func (r *Renderer) printf(format string, args ...any) {
	fmt.Fprintf(r.w, format, args...)
}

func writeLine(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ── JSON output ──────────────────────────────────────────────────────

type jsonEnvironment struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	CPUModel   string `json:"cpu_model,omitempty"`
	Goroutines int    `json:"goroutines"`
}

type jsonFeature struct {
	Name              string `json:"name"`
	Section           string `json:"section,omitempty"`
	Source            string `json:"source,omitempty"`
	Status            string `json:"status"`
	ElapsedNS         int64  `json:"elapsed_ns"`
	ElapsedText       string `json:"elapsed_text"`
	CompileAllocs     uint64 `json:"compile_allocs"`
	CompileAllocBytes uint64 `json:"compile_alloc_bytes"`
	RunAllocs         uint64 `json:"run_allocs"`
	RunAllocBytes     uint64 `json:"run_alloc_bytes"`
	Error             string `json:"error,omitempty"`
	SkipReason        string `json:"skip_reason,omitempty"`
}

type jsonSection struct {
	Name     string        `json:"name"`
	Features []jsonFeature `json:"features"`
}

type jsonReport struct {
	Environment jsonEnvironment `json:"environment"`
	Sections    []jsonSection   `json:"sections"`
	Passed      int             `json:"passed"`
	Failed      int             `json:"failed"`
	Skipped     int             `json:"skipped"`
	ElapsedNS   int64           `json:"elapsed_ns"`
	Score       Score           `json:"score"`
}

func runJSON(ctx context.Context, opts Options, cfg flowrunner.Config, sections []core.SelfTestSection) int {
	host := DetectHost()

	report := jsonReport{
		Environment: jsonEnvironment(host),
	}

	started := time.Now()
	var results []Result

	for _, s := range sections {
		js := jsonSection{Name: s.Name}
		for _, f := range s.Features {
			var res Result
			if f.Skip != "" {
				res = Result{SelfTestFeature: f, Status: StatusSkip, SkipReason: f.Skip, Section: s.Name}
			} else {
				res = runFeature(ctx, cfg, f)
				res.Section = s.Name
			}
			results = append(results, res)

			switch res.Status {
			case StatusPass:
				report.Passed++
			case StatusFail:
				report.Failed++
			case StatusSkip:
				report.Skipped++
			}
			jf := jsonFeature{
				Name:              f.Name,
				Section:           s.Name,
				Source:            f.Source,
				ElapsedNS:         res.Elapsed.Nanoseconds(),
				ElapsedText:       formatElapsed(res.Elapsed),
				CompileAllocs:     res.CompileAllocs,
				CompileAllocBytes: res.CompileAllocBytes,
				RunAllocs:         res.RunAllocs,
				RunAllocBytes:     res.RunAllocBytes,
				SkipReason:        res.SkipReason,
			}
			switch res.Status {
			case StatusPass:
				jf.Status = "passed"
			case StatusFail:
				jf.Status = "failed"
				if res.Err != nil {
					jf.Error = res.Err.Error()
				}
			case StatusSkip:
				jf.Status = "skipped"
			}
			js.Features = append(js.Features, jf)
		}
		report.Sections = append(report.Sections, js)
	}
	report.ElapsedNS = time.Since(started).Nanoseconds()
	report.Score = ComputeScore(results)

	if opts.SaveBaseline {
		if err := SaveBaseline(report.Score); err != nil {
			writeLine(opts.Out, "warning: could not save baseline: "+err.Error())
		}
	}

	enc := json.NewEncoder(opts.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		writeLine(opts.Out, "warning: could not encode report: "+err.Error())
	}

	if report.Failed > 0 {
		return 1
	}
	return 0
}

// UnmatchedFilters prints a red block after the summary listing every
// filter argument that matched no section and no feature.
func (r *Renderer) UnmatchedFilters(filters []string) {
	if len(filters) == 0 {
		return
	}

	line := strings.Repeat("─", 70)
	r.printf("\n%s\n", r.red(line))
	r.printf("  %s\n", r.red(r.bold("FILTERS NOT FOUND")))
	r.printf("%s\n", r.red(line))
	for _, f := range filters {
		r.printf("  %s  %s\n", r.red("✗"), r.red(f))
	}
	r.printf("%s\n", r.red(line))
	r.printf("  %s\n", r.dim("run `nflow self test` with no arguments to list every feature"))
	r.printf("%s\n", r.red(line))
}
