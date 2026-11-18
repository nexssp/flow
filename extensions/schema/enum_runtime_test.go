package schema_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	schemaext "github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestEnumSchemaFixtureUsesWireValuesAndDefaults(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(schemaext.Bundle(nil).Fixtures, "nflows/enum_defaults.nflow")
	if err != nil {
		t.Fatalf("read enum fixture: %v", err)
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	execution, err := runner.Execute(t.Context(), cfg, string(fixture), "enum-runtime.nflow", nil)
	if err != nil {
		t.Fatalf("execute enum fixture: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	if values["status"] != "settled" || values["refund_status"] != "canceled_by_user" {
		t.Fatalf("output = %#v, want enum wire values", values)
	}
}

func TestIncludedEnumRequiresAndAcceptsExplicitAlias(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	childPath := filepath.Join(root, "billing.nflow")
	childSource, err := fs.ReadFile(schemaext.Bundle(nil).Fixtures, "nflows/enum_import--billing.nflow")
	if err != nil {
		t.Fatalf("read included enum fixture: %v", err)
	}
	if err := os.WriteFile(childPath, childSource, 0o600); err != nil {
		t.Fatalf("write child source: %v", err)
	}
	rootPath := filepath.Join(root, "main.nflow")
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	base := `@include ./billing.nflow AS billing
@schema Order {
  status TYPE ` + "`json:\"status\" default:\"DEFAULT\"`" + `
}
{} -> Order.validate`
	qualified := strings.ReplaceAll(base, "TYPE", "billing.OrderStatus")
	qualified = strings.ReplaceAll(qualified, "DEFAULT", "billing.OrderStatus.paid")
	execution, err := runner.Execute(t.Context(), cfg, qualified, rootPath, nil)
	if err != nil {
		t.Fatalf("execute qualified enum: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok || values["status"] != "settled" {
		t.Fatalf("output = %#v, want default wire value", execution.Output)
	}
	qualifiedMember := strings.Replace(qualified, "{} -> Order.validate", "{ status: billing.OrderStatus.paid } -> Order.validate", 1)
	execution, err = runner.Execute(t.Context(), cfg, qualifiedMember, rootPath, nil)
	if err != nil {
		t.Fatalf("execute qualified enum member: %v", err)
	}
	values, ok = execution.Output.(map[string]any)
	if !ok || values["status"] != "settled" {
		t.Fatalf("output = %#v, want aliased enum member wire value", execution.Output)
	}

	unqualified := strings.ReplaceAll(base, "TYPE", "OrderStatus")
	unqualified = strings.ReplaceAll(unqualified, "DEFAULT", "OrderStatus.paid")
	_, err = runner.Execute(t.Context(), cfg, unqualified, rootPath, nil)
	if err == nil || !strings.Contains(err.Error(), "must be referenced through its @include alias") {
		t.Fatalf("execute unqualified imported enum error = %v, want explicit alias diagnostic", err)
	}
}
