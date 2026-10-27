package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/data"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
)

func TestRunnerInvocationOwnsPerRunCleanup(t *testing.T) {
	t.Parallel()
	cleanupErr := errors.New("cleanup failed")
	output, err := invokeWithRunScope(context.Background(), func(ctx context.Context) (any, error) {
		scope, ok := compiler.RunScopeFromContext(ctx)
		if !ok {
			t.Fatal("run scope is missing from invocation context")
		}
		if _, registerErr := scope.Register(func(context.Context) error { return cleanupErr }); registerErr != nil {
			t.Fatal(registerErr)
		}
		return "complete", nil
	})
	if output != "complete" {
		t.Fatalf("output = %v, want complete", output)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("invocation error = %v, want cleanup failure", err)
	}
}

func TestRunnerInvocationCleansUpBeforeReraisingPanic(t *testing.T) {
	t.Parallel()
	cleaned := false
	func() {
		defer func() {
			if recovered := recover(); recovered != "action panic" {
				t.Fatalf("recovered panic = %v, want action panic", recovered)
			}
		}()
		_, _ = invokeWithRunScope(context.Background(), func(ctx context.Context) (any, error) {
			scope, ok := compiler.RunScopeFromContext(ctx)
			if !ok {
				t.Fatal("run scope is missing from invocation context")
			}
			if _, err := scope.Register(func(context.Context) error {
				cleaned = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			panic("action panic")
		})
	}()
	if !cleaned {
		t.Fatal("resource cleanup did not run before panic was rethrown")
	}
}

func TestMaterializedPipelineInheritsAndReadsDataDeclarations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orders.jsonl"), []byte("{\"id\":\"one\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig([]compiler.Bundle{runtime.Bundle(nil), syntax.Bundle(nil), data.Bundle(nil), pipeline.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}
	source := "@data orders :from=\"orders.jsonl\"\n@pipeline child\n@datause orders\n-> data.select:fields=\"id\"\n-> collect\n@end\nruntime.const @{ value: {} } -> pipeline.child\n"
	execution, err := Execute(context.Background(), cfg, source, filepath.Join(dir, "flow.nflow"), map[string]any{})
	if err != nil {
		t.Fatalf("execute child pipeline with inherited @data declaration: %v", err)
	}
	rows, ok := execution.Output.([]any)
	if !ok {
		t.Fatalf("child output = %T (%#v), want []any", execution.Output, execution.Output)
	}
	if len(rows) != 1 {
		t.Fatalf("child output rows = %#v, want one row", rows)
	}
	row, ok := rows[0].(map[string]any)
	if !ok || row["id"] != "one" {
		t.Fatalf("child output row = %#v, want id one", rows[0])
	}
}

func TestDataMasterFixtureExecutesExpectedTrainingRows(t *testing.T) {
	t.Parallel()
	var feature compiler.SelfTestFeature
	found := false
	for _, section := range data.Bundle(nil).AllSelfTests() {
		for _, candidate := range section.Features {
			if candidate.Name == "Master example: deterministic training-data preparation from JSONL" {
				feature = candidate
				found = true
			}
		}
	}
	if !found {
		t.Fatal("master data fixture was not discovered")
	}
	dir := t.TempDir()
	for name, contents := range feature.Files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := BuildConfig([]compiler.Bundle{runtime.Bundle(nil), syntax.Bundle(nil), data.Bundle(nil), pipeline.Bundle(nil), assert.Bundle(nil), description.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := Execute(context.Background(), cfg, feature.DSL, filepath.Join(dir, "master_example.nflow"), map[string]any{})
	if err != nil {
		t.Fatalf("execute master data fixture: %v", err)
	}
	rows, ok := execution.Output.([]any)
	if !ok {
		t.Fatalf("fixture output = %T (%#v), want []any", execution.Output, execution.Output)
	}
	wantIDs := []string{"case-001", "case-003", "case-005", "case-007"}
	if len(rows) != len(wantIDs) {
		t.Fatalf("fixture output rows = %d, want %d", len(rows), len(wantIDs))
	}
	for i, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("fixture row %d = %T, want map[string]any", i, value)
		}
		if row["id"] != wantIDs[i] {
			t.Errorf("fixture row %d id = %v, want %s", i, row["id"], wantIDs[i])
		}
		if len(row) != 3 || row["amount"] != nil {
			t.Errorf("fixture row %d fields = %#v, want only id/text/label", i, row)
		}
	}
}
