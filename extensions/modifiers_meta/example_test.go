package modifiers_meta

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

type exampleRequest struct {
	AccountID string `json:"account_id" validate:"required"`
	Page      int    `json:"page" validate:"gt=0"`
}

func TestExampleModifierIsTypedDocumentationOnly(t *testing.T) {
	var calls atomic.Int32
	act := action.New("accounts.lookup", func(_ context.Context, req exampleRequest) (string, error) {
		calls.Add(1)
		return req.AccountID, nil
	}).Build()

	modified, err := compiler.NewModifierTable(Modifiers()...).ApplyAll(
		act,
		[]string{`example={"account_id":" acct-demo ","page":2}`},
	)
	if err != nil {
		t.Fatalf("ApplyAll() error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("modifier executed the action %d times", calls.Load())
	}

	got, ok := modified.Describe().Example.(exampleRequest)
	if !ok {
		t.Fatalf("Meta.Example has type %T, want exampleRequest", modified.Describe().Example)
	}
	if got.AccountID != " acct-demo " || got.Page != 2 {
		t.Fatalf("Meta.Example = %+v, want the original typed example payload", got)
	}

	out, err := action.InvokeAny(context.Background(), modified, map[string]any{
		"account_id": "runtime-request",
		"page":       3,
	})
	if err != nil {
		t.Fatalf("InvokeAny() error = %v", err)
	}
	if out != "runtime-request" {
		t.Fatalf("runtime output = %v, want runtime request result", out)
	}
	if calls.Load() != 1 {
		t.Fatalf("runtime call count = %d, want 1", calls.Load())
	}
}

func TestExampleModifierRejectsRequestSchemaMismatches(t *testing.T) {
	act := action.New("accounts.lookup", func(_ context.Context, req exampleRequest) (string, error) {
		return req.AccountID, nil
	}).Build()
	table := compiler.NewModifierTable(Modifiers()...)

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "invalid JSON", raw: `{"account_id":`, want: "expected valid JSON"},
		{name: "unknown field", raw: `{"account_id":"acct-demo","page":2,"typo":true}`, want: "unknown field"},
		{name: "wrong field type", raw: `{"account_id":7,"page":2}`, want: "cannot unmarshal number"},
		{name: "missing required field", raw: `{"page":2}`, want: "required"},
		{name: "invalid field constraint", raw: `{"account_id":"acct-demo","page":0}`, want: "gt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := table.ApplyAll(act, []string{"example=" + tc.raw})
			if err == nil {
				t.Fatal("ApplyAll() error = nil, want schema validation error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("ApplyAll() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
