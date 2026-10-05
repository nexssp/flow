// Package selftest runs the Nexss Flow self-test suite: a feature-by-
// feature check of every registered bundle, run in-process against the
// binary's own compiler and runtime.
//
// The bundle set the suite runs against is native.SelftestBundles() —
// the shipped set plus selftestkit. This is the only deviation from the
// production environment, and it is deliberate: fixtures across every
// bundle rely on cov.* helpers. Any other environment difference is a
// bug in the CLI, not a feature of self-test.
package selftest

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
	flowrunner "github.com/nexssp/flow/runner"
)

// Options configures a self-test run.
type Options struct {
	Out          io.Writer
	Filters      []string
	Verbose      bool
	NoColor      bool
	NoSpinner    bool
	JSON         bool
	SaveBaseline bool
}

// filterResult carries the outcome of one filter application.
type filterResult struct {
	Sections         []core.SelfTestSection
	UnmatchedFilters []string
}

// Run executes the suite and returns a process exit code: 0 when every
// selected feature passes, 1 when at least one fails, 2 on
// configuration error.
func Run(ctx context.Context, opts Options) int {
	bundles := native.SelftestBundles()
	host := flowrunner.NewHost()
	if err := host.OwnAll(bundles); err != nil {
		fmt.Fprintf(optionOutput(opts), "self test: host: %v\n", err)
		return 1
	}
	code := 1
	err := host.Run(ctx, func(runCtx context.Context) error {
		code = RunWithHost(runCtx, opts, bundles, host)
		return nil
	})
	if err != nil {
		fmt.Fprintf(optionOutput(opts), "%v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// RunWithHost executes the suite using bundles already owned by host. It is
// used by the CLI so self-test shares the command's native bundle instances and
// its outer invocation lifetime.
func RunWithHost(ctx context.Context, opts Options, bundles []core.Bundle, host *flowrunner.Host) int {
	_ = host // ownership is established by the caller; retained in the contract.
	return runWithBundles(ctx, opts, bundles)
}

func optionOutput(opts Options) io.Writer {
	if opts.Out != nil {
		return opts.Out
	}
	return os.Stdout
}

func runWithBundles(ctx context.Context, opts Options, bundles []core.Bundle) int {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if !isTerminal(opts.Out) {
		opts.NoColor = true
		opts.NoSpinner = true
	}

	all := SectionsFromBundles(bundles)
	res := filterSections(all, opts.Filters)

	if len(res.Sections) == 0 {
		writeLine(opts.Out, "no features matched: "+strings.Join(opts.Filters, ", "))
		writeLine(opts.Out, "")
		writeLine(opts.Out, "try: nflow self test              (list everything)")
		writeLine(opts.Out, "     nflow self test loop         (only the loop bundle)")
		writeLine(opts.Out, "     nflow self test loop assert  (any bundle or feature containing 'loop' or 'assert')")
		return 2
	}

	cfg, err := flowrunner.BuildConfig(bundles)
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
