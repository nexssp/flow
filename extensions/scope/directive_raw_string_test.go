package scope

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestScopeBodyKeepsProjectionBoundaryAfterRawBacktick(t *testing.T) {
	t.Parallel()

	lines := []string{
		`@scope :retry=3 {`,
		`  source`,
		`  -> {`,
		"    text: `raw\\` + `}`",
		`  }`,
		`  runtime.noop`,
		`}`,
	}
	out := map[string]any{}
	res, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "raw-backtick.nflow",
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res.BlankLines, []int{0, 6})

	spans := spansFromMeta(out)
	ktest.RequireEqual(t, len(spans), 1)
	ktest.RequireEqual(t, spans[0].end, 6)
}
