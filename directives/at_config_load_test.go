package directives_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow"
)

func TestDirective_ConfigLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// 1. Utworzenie zewnętrznego pliku YAML
	yamlContent := "timeout: 10s\nretries: 3\ndatabase:\n  host: 127.0.0.1\n"
	yamlPath := filepath.Join(dir, "settings.yml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Utworzenie potoku .nflow z @config.load
	nflowContent := `@config.load:path="settings.yml" :prefix="app"
@config.load:path="missing.json" :required=false

const @{ value: "ok" }
`
	nflowPath := filepath.Join(dir, "pipeline.nflow")
	if err := os.WriteFile(nflowPath, []byte(nflowContent), 0o600); err != nil {
		t.Fatal(err)
	}

	pre, err := flow.Preprocess(nflowPath)
	if err != nil {
		t.Fatalf("Preprocess failed: %v", err)
	}

	// 3. Weryfikacja wstrzykniętych wartości konfiguracyjnych
	if pre.Config["app.timeout"] != "10s" {
		t.Errorf("got app.timeout=%q, want 10s", pre.Config["app.timeout"])
	}
	if pre.Config["app.retries"] != "3" {
		t.Errorf("got app.retries=%q, want 3", pre.Config["app.retries"])
	}
	if pre.Config["app.database.host"] != "127.0.0.1" {
		t.Errorf("got app.database.host=%q, want 127.0.0.1", pre.Config["app.database.host"])
	}
}
