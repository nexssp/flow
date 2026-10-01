// Package selftest runs the Nexss Flow self-test suite: a feature-by-
// feature check of every registered bundle, run in-process against the
// binary's own compiler and runtime.
//
// It is designed to be embedded: `nflow self test` prints a live,
// category-colored checklist and exits 0 only when every feature
// passes. Both inline SelfTest() sections and *.nflow fixtures embedded
// by each bundle (Bundle.Fixtures) are discovered automatically and
// executed identically regardless of working directory or installed
// files.
package selftest

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nexssp/flow/core"
	flowrunner "github.com/nexssp/flow/runner"
)

// Options configures a self-test run.
type Options struct {
	Out          io.Writer // terminal output (default os.Stdout)
	Filters      []string  // case-insensitive substrings; empty = all sections
	Verbose      bool      // print DSL and full failure details
	NoColor      bool      // disable ANSI color
	NoSpinner    bool      // disable live updates (for CI logs)
	JSON         bool      // emit machine-readable JSON summary instead of text
	SaveBaseline bool      // write .nflow_selftest_baseline.json after the run
}

// filterResult carries the outcome of one filter application.
type filterResult struct {
	Sections []core.SelfTestSection
	// UnmatchedFilters are the input filters that matched no section
	// and no feature. Reported to the user at the end of the run.
	UnmatchedFilters []string
}

// Run executes the suite and returns a process exit code: 0 when every
// selected feature passes, 1 when at least one fails, 2 on
// configuration error (bad filter matched nothing).
//
// The runner configuration is built exactly once. It is shared across
// every feature; fixtures are expected to be self-contained (each one
// declares its own @pipeline / @pool names). A fixture that references
// a name declared only by another fixture would still compile here,
// because the resolver is shared — the same way production behaves
// when two .nflow files are loaded into one process.
func Run(ctx context.Context, opts Options) int {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if !isTerminal(opts.Out) {
		opts.NoColor = true
		opts.NoSpinner = true
	}

	all := Sections()
	res := filterSections(all, opts.Filters)

	if len(res.Sections) == 0 {
		writeLine(opts.Out, "no features matched: "+strings.Join(opts.Filters, ", "))
		writeLine(opts.Out, "")
		writeLine(opts.Out, "try: nflow self test              (list everything)")
		writeLine(opts.Out, "     nflow self test loop         (only the loop bundle)")
		writeLine(opts.Out, "     nflow self test loop assert  (any bundle or feature containing 'loop' or 'assert')")
		return 2
	}

	cfg, err := flowrunner.BuildConfig(core.RegisteredBundles())
	if err != nil {
		fmt.Fprintf(opts.Out, "self test: build config: %v\n", err)
		return 1
	}
	cfg.MeasureAllocs = true

	if opts.JSON {
		return runJSON(ctx, opts, cfg, res.Sections)
	}

	r := newRenderer(opts)
	r.Banner(res.Sections)

	started := time.Now()
	var results []Result

	for _, s := range res.Sections {
		r.SectionStart(s)
		sectionStart := time.Now()
		for _, f := range s.Features {
			if f.Skip != "" {
				skipRes := Result{
					SelfTestFeature: f,
					Section:         s.Name,
					Status:          StatusSkip,
					SkipReason:      f.Skip,
				}
				r.FeatureStart(f)
				r.FeatureDone(skipRes)
				results = append(results, skipRes)
				continue
			}
			r.FeatureStart(f)
			runRes := runFeature(ctx, cfg, f)
			runRes.Section = s.Name
			r.FeatureDone(runRes)
			results = append(results, runRes)
		}
		r.SectionDone(s, results, time.Since(sectionStart))
	}

	r.Summary(results, time.Since(started))

	score := ComputeScore(results)
	r.TopMetrics(score)
	r.ScoreSummary(score, DetectHost())

	if opts.SaveBaseline {
		if err := SaveBaseline(score); err != nil {
			writeLine(opts.Out, "warning: could not save baseline: "+err.Error())
		}
	}

	r.UnmatchedFilters(res.UnmatchedFilters)

	for i := range results {
		if results[i].Status == StatusFail {
			return 1
		}
	}
	return 0
}

// filterSections keeps sections and features that match any of the
// given case-insensitive substrings.
//
// Matching rules, per filter:
//   - a filter that matches a section's Name keeps the whole section
//   - a filter that only matches individual features keeps those features
//   - a filter that matches nothing is reported back to the caller as
//     "unmatched" so the renderer can list it in the summary
//
// Every filter is ORed with the others, so `nflow self test loop assert`
// shows every loop-related and every assert-related feature.
//
// Section names are bundle IDs ("loop", "macros", "modifiers_core");
// feature names are either the inline Name declared by SelfTest() or
// the @description value of a *.nflow fixture.
func filterSections(in []core.SelfTestSection, filters []string) filterResult {
	norm := make([]string, 0, len(filters))
	for _, f := range filters {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			norm = append(norm, f)
		}
	}
	if len(norm) == 0 {
		return filterResult{Sections: in}
	}

	hit := make(map[string]bool, len(norm))

	matches := func(s string) bool {
		s = strings.ToLower(s)
		for _, f := range norm {
			if strings.Contains(s, f) {
				hit[f] = true
				return true
			}
		}
		return false
	}

	out := make([]core.SelfTestSection, 0, len(in))
	for _, s := range in {
		if matches(s.Name) {
			out = append(out, s)
			continue
		}
		kept := make([]core.SelfTestFeature, 0, len(s.Features))
		for _, f := range s.Features {
			if matches(f.Name) {
				kept = append(kept, f)
			}
		}
		if len(kept) > 0 {
			out = append(out, core.SelfTestSection{Name: s.Name, Features: kept})
		}
	}

	var unmatched []string
	for _, f := range norm {
		if !hit[f] {
			unmatched = append(unmatched, f)
		}
	}
	return filterResult{Sections: out, UnmatchedFilters: unmatched}
}
