package template_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestRuntimeFileContentIsTemplateSource(t *testing.T) {
	dir := t.TempDir()
	const templateText = "Welcome {{.name}} — Grüß Gott, 世界 🌍\n"
	if err := os.WriteFile(filepath.Join(dir, "welcome.tmpl"), []byte(templateText), 0o600); err != nil {
		t.Fatal(err)
	}

	source := fmt.Sprintf(`@assert: result.succeeded == 1
@assert: result.items[0].ok == true
@assert: result.items[0].result == "Welcome Ada — Grüß Gott, 世界 🌍\\n"
@pipeline render_runtime_template_file
  template.render ( template_bytes: .Content, variables: { name: "Ada" } )
@end
fs.walk ( dir: %q, ext: "tmpl" ) -> fs.read -> collect -> { files: . } -> distribute.map ( action: pipeline.render_runtime_template_file, items: .files )`, dir)
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Execute(t.Context(), cfg, source, "runtime_file.nflow", nil)
	if err != nil {
		t.Fatal(err)
	}
}
