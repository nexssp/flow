package pipeline

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/scope"
)

func TestPipelineMultilineHeaderTriviaAndBodyBoundary(t *testing.T) {
	t.Parallel()
	source := `@profile base
  :retry=2

@pipeline process_order

  # Header remains open across trivia.
  :schema=OrderRequest
  :route="POST /api/v1/orders"
  // Policy continuation.
  :profile=base
  :rate_limit=100 :idempotent
  :timeout=5s

  order.validate
    :timeout=1s
    :dedup
  -> order.charge
  -> order.ship
@end
@pipeline next
  runtime.noop
@end
`
	clean, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(scope.ProfileDirective, Directive, EndDirective), source, "orders.nflow")
	ktest.RequireNoError(t, err)
	mods, ok := meta["pipeline_modifiers"].(map[string][]string)
	ktest.RequireCondition(t, ok, "pipeline modifiers missing")
	ktest.RequireEqual(t, mods["process_order"], []string{"schema=OrderRequest", "route=POST /api/v1/orders", "retry=2", "rate_limit=100", "idempotent", "timeout=5s"})
	bodies, ok := meta["pipelines"].(map[string]string)
	ktest.RequireCondition(t, ok, "pipeline bodies missing")
	ktest.RequireEqual(t, bodies["process_order"], "\n  order.validate\n    :timeout=1s\n    :dedup\n  -> order.charge\n  -> order.ship")
	ktest.RequireEqual(t, bodies["next"], "  runtime.noop")
	ktest.RequireEqual(t, strings.Count(clean, "\n"), strings.Count(source, "\n"))
}

func TestPipelineMultilineHeaderRejectsMalformedValue(t *testing.T) {
	t.Parallel()
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(Directive), "@pipeline work\n\n  :timeout= :dedup\n  runtime.noop\n@end", "pipeline.nflow")
	ktest.RequireErrorContains(t, err, "pipeline.nflow:3")
}
