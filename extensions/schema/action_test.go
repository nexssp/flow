package schema_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/runner"
)

func TestSchemaValidate_PassAndFail(t *testing.T) {
	t.Parallel()

	bundles := []core.Bundle{
		runtime.Bundle(nil),
		schema.Bundle(nil),
	}
	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)

	dsl := `
@schema IntakeCase struct {
  CaseRef     string ` + "`json:\"case_ref\" validate:\"required\"`" + `
  ReportCount int    ` + "`json:\"report_count\" validate:\"required\"`" + `
  Active      bool   ` + "`json:\"active\"`" + `
}

schema.validate @{ name: "IntakeCase" }
`

	ctx := context.Background()

	// 1. Valid payload passes
	validInput := map[string]any{
		"case_ref":     "CASE-01",
		"report_count": 2,
		"active":       true,
	}
	res, err := runner.Execute(ctx, cfg, dsl, "valid", validInput)
	ktest.RequireNoError(t, err)
	outMap, ok := res.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map output")
	ktest.RequireEqual(t, outMap["case_ref"], "CASE-01")

	// 2. Missing required field fails with Validation kind
	invalidInput := map[string]any{
		"report_count": 2,
	}
	_, err = runner.Execute(ctx, cfg, dsl, "invalid", invalidInput)
	ktest.RequireCondition(t, err != nil, "expected validation error")
	ktest.RequireEqual(t, xerr.KindFrom(err), xerr.KindValidation)
}
