package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner"
)

func init() {
	compiler.Register("bundle-source-cli-test", func(map[string]string) compiler.Bundle {
		prompt := action.New("ai.prompt", func(context.Context, any) (any, error) {
			return "action from the required bundle", nil
		}).Build()
		return compiler.Bundle{
			ID:        "bundle-source-cli-test",
			Libraries: []action.Library{{Name: "ai", Actions: []action.AnyAction{prompt}}},
			SourceModules: fstest.MapFS{
				"workflows/critic.nflow": &fstest.MapFile{Data: []byte("@require example.invalid/bundle-source-cli-test v1.0.0 as ai\n@pipeline review\n  ai.prompt\n@end\n")},
			},
			ExportedModules: []string{"workflows/critic.nflow"},
		}
	})
}

func TestPrepareRequiredSourceLoadsBundleModuleAfterRequirementDiscovery(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatalf("newNativeInvocation: %v", err)
	}
	const source = `@require example.invalid/bundle-source-cli-test v1.0.0 as ai
@include ai:workflows/critic.nflow AS critic

critic.pipeline.review`
	name := filepath.Join(t.TempDir(), "bundle-source-cli.nflow")
	if err := os.WriteFile(name, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	var output any
	var importName string
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		requirements, discoveryErr := inv.requirementsForHarness(ctx, name)
		if discoveryErr != nil {
			return discoveryErr
		}
		if len(requirements) != 1 || requirements[0].Version != "v1.0.0" {
			return fmt.Errorf("harness requirements = %#v, want one v1.0.0 requirement", requirements)
		}
		_, _, cfg, prepareErr := inv.prepareRequiredSource(ctx, source, name)
		if prepareErr != nil {
			return prepareErr
		}
		execution, executeErr := runner.Execute(ctx, cfg, source, name, map[string]any{})
		if executeErr != nil {
			return executeErr
		}
		output = execution.Output
		imports := compiler.SourceImports(execution.Meta)
		if len(imports) != 1 {
			return fmt.Errorf("source imports = %d, want 1", len(imports))
		}
		importName, _ = imports[0].Source.SourceIdentity()
		return nil
	})
	if err != nil {
		t.Fatalf("host run: %v", err)
	}
	if output != "action from the required bundle" {
		t.Fatalf("output = %#v, want the required bundle action's result", output)
	}
	if importName != "bundle://bundle-source-cli-test/workflows/critic.nflow" {
		t.Fatalf("import source identity = %q, want required bundle module URI", importName)
	}
}
