package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestAliasedBundleArgumentSchemasRemainEnforced(t *testing.T) {
	t.Parallel()
	wrap := action.New("tools.wrap", func(_ context.Context, input any) (any, error) {
		return input, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "aliased-schema-test",
		Alias:     "local",
		Libraries: []action.Library{{Name: "tools", Actions: []action.AnyAction{wrap}}},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"tools.wrap": {{Name: "action", Kind: compiler.ArgCapabilityRef}},
		},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	if _, err := runner.Compile(t.Context(), cfg, "local.wrap @{ action: runtime.noop }", "aliased-schema.nflow"); err != nil {
		t.Fatalf("valid aliased capability reference failed to compile: %v", err)
	}

	for _, tc := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "required field", source: "local.wrap @{}", want: "required capability reference"},
		{name: "quoted reference", source: `local.wrap @{ action: "runtime.noop" }`, want: "use a bare capability reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := runner.Compile(t.Context(), cfg, tc.source, "aliased-schema.nflow"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compile() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
