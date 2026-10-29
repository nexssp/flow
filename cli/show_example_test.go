package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

type showExampleRequest struct {
	AccountID string `json:"account_id"`
}

func TestShowAtomPrintsTypedKernelExample(t *testing.T) {
	act := action.New("example.echo", func(_ context.Context, req showExampleRequest) (string, error) {
		return req.AccountID, nil
	}).Example(showExampleRequest{AccountID: "acct-demo"}).Build()

	resolver, err := compiler.NewDynamicResolver(action.Library{
		Name:    "show_example_test",
		Actions: []action.AnyAction{act},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolver() error = %v", err)
	}

	catalog := compiler.BuildCatalog(
		resolver,
		compiler.NewModifierTable(),
		compiler.NewDirectiveTable(),
		compiler.NewOperatorTable(),
	)
	stdout, _ := captureIO(t, func() {
		if !showAtom(catalog.Atoms, "example.echo", false) {
			t.Error("showAtom() did not find example.echo")
		}
	})
	for _, want := range []string{"EXAMPLE", `"account_id": "acct-demo"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show output missing %q:\n%s", want, stdout)
		}
	}
}
