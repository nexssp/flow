package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
	"github.com/nexssp/flow/native"
)

// TestSecretCaptureHookScrubsLibraryExecution guards the library execution path
// used by embedders such as srcpack: no observer, no event sink, and no caller
// hooks. Secret capture must still run through the runner's capture hook so the
// execution-scoped scrubber can redact echoed secrets from the final output.
func TestSecretCaptureHookScrubsLibraryExecution(t *testing.T) {
	const token = "library-capture-canary"
	consume := action.New("test.consume_secret", func(_ context.Context, input map[string]any) (map[string]any, error) {
		value, ok := input["api_key"].(secret.Value)
		if !ok {
			return nil, fmt.Errorf("api_key type = %T, want secret.Value", input["api_key"])
		}
		var actual string
		if err := secret.WithValue(value, func(raw []byte) error {
			actual = string(raw)
			return nil
		}); err != nil {
			return nil, err
		}
		if actual != token {
			return nil, errors.New("api_key did not reach the explicitly typed action")
		}
		return map[string]any{"accepted": true, "echo": "server echoed " + actual}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "library-secret-capture-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{consume}}},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"test.consume_secret": {{Name: "api_key", Kind: compiler.ArgSecret}},
		},
	}
	cfg, err := BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	source := "@schema Request {\n  APIKey secret `json:\"api_key\"`\n}\n@pipeline handle\n  :request-schema=Request\n  test.consume_secret(api_key: .api_key)\n@end\npipeline.handle\n"
	execution, err := Execute(t.Context(), cfg, source, "library-secret-capture.nflow", map[string]any{"api_key": token})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rendered := fmt.Sprintf("%#v", execution.Output)
	if strings.Contains(rendered, token) {
		t.Fatalf("library execution output leaked the echoed Secret: %s", rendered)
	}
	if !strings.Contains(rendered, secret.Redacted) {
		t.Fatalf("library execution output missed the redaction marker: %s", rendered)
	}
}
