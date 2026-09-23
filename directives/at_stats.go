package directives

import (
	"strings"

	"github.com/nexssp/flow/directives/core"
)

const StatsDeclarationKey = "stats_collectors"

type atStats struct{}

func init() { Register(atStats{}) }

func (atStats) Name() string { return "stats" }

func (atStats) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	var items []string
	if strings.Contains(line, "{") {
		_, body, next, err := SplitBlock(lines, i)
		if err != nil {
			return 0, AtErr(ctx, i, "stats", err.Error())
		}
		for _, bLine := range body {
			trimmed := strings.TrimSpace(bLine)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, part := range core.SplitTopLevel(trimmed, ',') {
				part = TrimQuotes(strings.TrimSpace(part))
				if part != "" {
					items = append(items, strings.ToLower(part))
				}
			}
		}
		saveStatsDeclaration(ctx, items)
		return next, nil
	}

	spec, ok := StripDirectivePrefix(line, "stats")
	if !ok {
		return 0, AtErr(ctx, i, "stats", "malformed directive")
	}

	for _, part := range ParseList(spec) {
		items = append(items, strings.ToLower(part))
	}

	saveStatsDeclaration(ctx, items)
	return i + 1, nil
}

func saveStatsDeclaration(ctx *Context, items []string) {
	if len(items) == 0 {
		items = []string{"files", "bytes", "tokens", "duration"}
	}
	if ctx.Out.Declarations == nil {
		ctx.Out.Declarations = make(map[string]any)
	}
	ctx.Out.Declarations[StatsDeclarationKey] = items
}
