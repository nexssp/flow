package directives

import (
	"path/filepath"
	"strings"
)

type atInclude struct{}

func init() { Register(atInclude{}) }

func (atInclude) Name() string { return "include" }

// Syntax:
//
//	@include ./other.nflow
//
// The included file's DSL is inlined as a parenthesised expression on
// the current body line, so its nodes participate in the current
// pipeline's topology. Its pipelines and requires are merged into the
// current file. Profile declarations must agree.
func (atInclude) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	ref, ok := StripDirectivePrefix(line, "include")
	if !ok {
		return 0, AtErr(ctx, i, "include", "malformed directive")
	}
	ref = TrimQuotes(ref)

	if ref == "" {
		return 0, AtErr(ctx, i, "include", "requires a path")
	}

	if ctx.IncludeResolver == nil {
		return 0, AtErr(ctx, i, "include", "not available in current mode (IncludeResolver is nil)")
	}

	incPath := ref
	if !filepath.IsAbs(incPath) && ctx.BaseDir != "" {
		incPath = filepath.Join(ctx.BaseDir, incPath)
	}

	inc, err := ctx.IncludeResolver(incPath)
	if err != nil {
		return 0, AtErrf(ctx, i, "include", "failed resolving %q: %v", ref, err)
	}

	merged, err := mergeProfile(ctx.Out.Profile, inc.Profile, incPath)
	if err != nil {
		return 0, AtErr(ctx, i, "include", err.Error())
	}
	ctx.Out.Profile = merged

	for _, p := range inc.Pipelines {
		if _, exists := findPipeline(ctx.Out.Pipelines, p.Name); exists {
			return 0, AtErrf(ctx, i, "include "+ref, "pipeline %q conflicts with an existing declaration", p.Name)
		}
		ctx.Out.Pipelines = append(ctx.Out.Pipelines, p)
	}

	for _, r := range inc.Requires {
		ctx.Out.Requires = appendUniqueRequirement(ctx.Out.Requires, r)
	}

	if s := strings.TrimSpace(inc.DSL); s != "" {
		var kept []string
		for _, line := range strings.Split(s, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "@") {
				continue
			}
			kept = append(kept, trimmed)
		}
		if len(kept) > 0 {
			ctx.Body[i] = "(" + strings.Join(kept, " -> ") + ")"
		}
	}

	return i + 1, nil
}
