package schema_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func advisorConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		pipeline.Bundle(nil),
		schema.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func TestSchemaRef_KnownSchemaPasses(t *testing.T) {
	t.Parallel()
	cfg := advisorConfig(t)

	src := `@schema User { Name string }
const @{ value: "x" }:schema=User`
	_, err := runner.Compile(t.Context(), cfg, src, "ok.nflow")
	ktest.RequireNoError(t, err)
}

func TestSchemaRef_UnknownSchemaRejected(t *testing.T) {
	t.Parallel()
	cfg := advisorConfig(t)

	src := `@schema User { Name string }
const @{ value: "x" }:schema=Usr`
	_, err := runner.Compile(t.Context(), cfg, src, "bad.nflow")
	ktest.RequireErrorContains(t, err, `:schema=Usr: unknown schema`)
	ktest.RequireErrorContains(t, err, "declared: User")
	ktest.RequireErrorContains(t, err, "bad.nflow:")
}

func TestSchemaRef_EmptyValueRejected(t *testing.T) {
	t.Parallel()
	cfg := advisorConfig(t)

	// `:schema=` with no value is rejected by the parser before the
	// schema advisor runs — the modifier value slot is empty, and
	// parseModifier refuses to produce an atom without a value.
	// The message names the position and the expected token.
	src := `@schema User { Name string }
const @{ value: "x" }:schema=`
	_, err := runner.Compile(t.Context(), cfg, src, "empty.nflow")
	ktest.RequireErrorContains(t, err, "expected value")
}

func TestSchemaRef_NoSchemasDeclaredIsNoop(t *testing.T) {
	t.Parallel()
	cfg := advisorConfig(t)

	// No @schema declarations at all: the advisor must not be installed
	// and the modifier must remain accepted (backward compatible).
	src := `const @{ value: "x" }:schema=Anything`
	_, err := runner.Compile(t.Context(), cfg, src, "noop.nflow")
	ktest.RequireNoError(t, err)
}

func TestSchemaRef_PipelineBodyInheritsParentSchemas(t *testing.T) {
	t.Parallel()
	cfg := advisorConfig(t)

	src := `@schema User { Name string }

@pipeline work
  const @{ value: "x" }:schema=User
@end

pipeline.work`
	_, err := runner.Compile(t.Context(), cfg, src, "pipeline-ok.nflow")
	ktest.RequireNoError(t, err)
}

func TestSchemaRef_PipelineBodyTypoDetected(t *testing.T) {
	t.Parallel()
	cfg := advisorConfig(t)

	src := `@schema User { Name string }

@pipeline work
  const @{ value: "x" }:schema=Usr
@end

pipeline.work`
	_, err := runner.Compile(t.Context(), cfg, src, "pipeline-bad.nflow")
	ktest.RequireErrorContains(t, err, `:schema=Usr: unknown schema`)
}
