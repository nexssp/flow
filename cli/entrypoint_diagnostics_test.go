package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRunSource_PipedJSONWithPipelineEntrypoint(t *testing.T) {
	withStdin(t, `{"principal_id":"demo-user","turn_id":"turn-001","prompt":"Remember tea."}`)
	source := `@pipeline chat_with_memory
  runtime.noop
@end

{
  principal_id: .principal_id,
  turn_id: .turn_id,
  prompt: .prompt
}
-> pipeline.chat_with_memory
`

	code := 0
	stdout, stderr := captureIO(t, func() {
		code = runSourceInProcess(t.Context(), source, "memory_backends_step2.nflow", []string{"--json"})
	})
	if code != 0 {
		t.Fatalf("run returned %d; stderr=%q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode output %q: %v", stdout, err)
	}
	want := map[string]any{
		"principal_id": "demo-user",
		"turn_id":      "turn-001",
		"prompt":       "Remember tea.",
	}
	if len(got) != len(want) {
		t.Fatalf("output = %#v, want %#v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("output[%q] = %#v, want %#v", key, got[key], value)
		}
	}
}

func TestRunSource_DeclarationOnlyVerboseErrorExplainsMissingEntrypoint(t *testing.T) {
	withStdin(t, "")
	source := `@pipeline chat_with_memory
  runtime.noop
@end
`

	code := 0
	_, stderr := captureIO(t, func() {
		code = runSourceInProcess(t.Context(), source, "declaration_only.nflow", []string{"-v"})
	})
	if code != 2 {
		t.Fatalf("run returned %d, want compile/preflight exit code 2; stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"no runnable Flow expression remained after preprocessing",
		"add a top-level call",
		"nflow info <file>",
		"debug: compile finished in ",
		"execution did not start; no runtime action events are expected",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}
