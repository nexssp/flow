package cli

import (
	"context"
	"strings"
	"testing"
)

func TestShowExampleModifierDocumentsJSONSyntax(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatalf("newNativeInvocation() error = %v", err)
	}
	var code int
	stdout, _ := captureIO(t, func() {
		code = runShowInProcess(context.Background(), inv, []string{"example"})
	})
	if code != 0 {
		t.Fatalf("runShowInProcess() code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `:example='{"account_id":"acct-demo"}'`) {
		t.Fatalf("show output does not document JSON example syntax:\n%s", stdout)
	}
}
