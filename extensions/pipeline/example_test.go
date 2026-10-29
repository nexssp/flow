package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	schemaext "github.com/nexssp/flow/extensions/schema"
)

func TestPipelineExampleValidatedAgainstRequestSchema(t *testing.T) {
	for _, tc := range []struct {
		name      string
		example   string
		strict    bool
		wantError string
	}{
		{name: "valid sample", example: `{"account_id":"acct-demo"}`},
		{name: "missing required field", example: `{"other":"value"}`, wantError: "required"},
		{name: "strict unknown field", example: `{"account_id":"acct-demo","other":"value"}`, strict: true, wantError: "unknown fields"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := compiler.NewDynamicResolver()
			if err != nil {
				t.Fatalf("NewDynamicResolver() error = %v", err)
			}
			var calls int
			compile := func(name, _ string, mods ...string) (action.AnyAction, error) {
				act := action.New(name, func(_ context.Context, input any) (any, error) {
					calls++
					return input, nil
				}).Build()
				return compiler.NewModifierTable(modifiers_meta.Modifiers()...).ApplyAll(act, mods)
			}
			selection := "Request"
			if tc.strict {
				selection = "!" + selection
			}
			req := compiler.MaterializeReq{
				Ctx: context.Background(),
				Meta: map[string]any{
					"schemas": []schemaext.Schema{{
						Name: "Request",
						Fields: []schemaext.Field{{
							Name: "AccountID", JSONName: "account_id", Type: "string",
							Kind: schemaext.KindString, Tags: map[string]string{"validate": "required"},
						}},
					}},
				},
				Resolver: resolver,
				Compile:  compile,
			}
			definition := pipelineDefinition{
				name:   "lookup",
				source: "noop",
				modifiers: []string{
					"request-schema=" + selection,
					"example=" + tc.example,
				},
			}
			_, err = materializePipeline(req, definition, map[string]struct{}{})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantError)) {
					t.Fatalf("materializePipeline() error = %v, want it to contain %q", err, tc.wantError)
				}
				if _, ok := resolver.Action("pipeline.lookup"); ok {
					t.Fatal("invalid example pipeline was mounted")
				}
			} else {
				if err != nil {
					t.Fatalf("materializePipeline() error = %v", err)
				}
				act, ok := resolver.Action("pipeline.lookup")
				if !ok {
					t.Fatal("valid example pipeline was not mounted")
				}
				example, ok := act.Describe().Example.(map[string]any)
				if !ok || example["account_id"] != "acct-demo" {
					t.Fatalf("pipeline Meta.Example = %#v, want request object", act.Describe().Example)
				}
			}
			if calls != 0 {
				t.Fatalf("materialization executed pipeline body %d times", calls)
			}
		})
	}
}
