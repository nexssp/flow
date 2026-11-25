package builder

import (
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

func TestPrepareBundleImportsRejectsSamePackageWithDifferentAliases(t *testing.T) {
	const importPath = "example.invalid/flow/nexssflow"
	requirements := []require.Requirement{
		{Import: importPath, PackagePath: importPath, Alias: "first"},
		{Import: importPath, PackagePath: importPath, Alias: "second"},
	}
	_, err := prepareBundleImports(t.TempDir(), requirements)
	if err == nil {
		t.Fatal("prepareBundleImports() accepted the same package under two aliases")
	}
	for _, want := range []string{importPath, "first", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("prepareBundleImports() error = %q, want it to contain %q", err, want)
		}
	}
}
