package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
	"github.com/nexssp/flow/native"
)

func TestSecretDiagnosticDoesNotEchoSourceSnippet(t *testing.T) {
	const token = "sensitive-canary"
	source := `bearer: "` + token + `"`
	err := compiler.SourceError(compiler.Position{File: "secret.nflow", Line: 1, Col: 10}, "secret parameter requires a typed value")
	detail := FormatErrorDetail(err, source, "secret.nflow", false)
	if strings.Contains(detail, token) {
		t.Fatalf("Secret diagnostic echoed source literal: %s", detail)
	}
	if !strings.Contains(detail, "secret.nflow:1:10") {
		t.Fatalf("Secret diagnostic lost its source location: %s", detail)
	}
}

func TestPipelineSecretSinkRedactsFinalOutput(t *testing.T) {
	const token = "pipeline-secret-canary"
	var fail bool
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
		if fail {
			return nil, fmt.Errorf("downstream error echoed %s", actual)
		}
		return map[string]any{"accepted": true, "echo": "server echoed " + actual}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "pipeline-secret-sink-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{consume}}},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"test.consume_secret": {{Name: "api_key", Kind: compiler.ArgSecret}},
		},
	}
	cfg, err := BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	hookCalls := 0
	cfg.Hooks = append(cfg.Hooks, action.AnyHook{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			hookCalls++
			return ctx, nil
		},
	})
	source := `@schema Request {
  APIKey secret ` + "`json:\"api_key\"`" + `
}
@pipeline handle
  :request-schema=Request
  test.consume_secret(api_key: .api_key)
@end
pipeline.handle
`
	var stdout, stderr bytes.Buffer
	code := RunSource(t.Context(), cfg, source, "pipeline-secret-sink.nflow", map[string]any{"api_key": token}, Opts{
		Verbosity: 3,
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if code != 0 {
		t.Fatalf("RunSource exit code = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
		t.Fatalf("final output leaked raw request Secret: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), secret.Redacted) {
		t.Fatalf("echoed final result omitted the redaction marker: stdout=%s", stdout.String())
	}
	if hookCalls == 0 {
		t.Fatal("configured action hook did not run")
	}
	if !strings.Contains(stdout.String(), "<omitted; intermediate payload>") {
		t.Fatalf("verbose trace did not omit intermediate payloads: %s", stdout.String())
	}
	fail = true
	stdout.Reset()
	stderr.Reset()
	code = RunSource(t.Context(), cfg, source, "pipeline-secret-sink.nflow", map[string]any{"api_key": token}, Opts{
		Verbosity: 3,
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if code == 0 {
		t.Fatal("RunSource succeeded after the downstream action returned an error")
	}
	if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
		t.Fatalf("error diagnostics leaked raw request Secret: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestIncludedPipelineSecretContractSurvivesNamespaceMount(t *testing.T) {
	const token = "included-pipeline-secret-canary"
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
		return map[string]any{"accepted": true, "echo": "included " + token}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "included-pipeline-secret-sink-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{consume}}},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"test.consume_secret": {{Name: "api_key", Kind: compiler.ArgSecret}},
		},
	}
	cfg, err := BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	dir := t.TempDir()
	rootFile := filepath.Join(dir, "main.nflow")
	moduleFile := filepath.Join(dir, "secret_module.nflow")
	module := `@schema Request {
  APIKey secret ` + "`json:\"api_key\"`" + `
}
@pipeline handle
  :request-schema=Request
  test.consume_secret(api_key: .api_key)
@end
`
	if err := os.WriteFile(moduleFile, []byte(module), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	include := "@include ./secret_module.nflow AS secrets\n"
	compiled, err := Compile(t.Context(), cfg, include+"secrets.pipeline.handle", rootFile)
	if err != nil {
		t.Fatalf("compile included Secret pipeline: %v", err)
	}
	provider, ok := compiled.Resolver.(compiler.ContractProvider)
	if !ok {
		t.Fatal("compiled resolver does not expose action contracts")
	}
	contract, ok := provider.Contract("secrets.pipeline.handle")
	if !ok || !contract.SecretBoundary || !contract.SecretInputNormalizer {
		t.Fatalf("qualified Secret pipeline contract = %#v, found=%t", contract, ok)
	}
	invalidSource := include + `secrets.pipeline.handle(api_key: "` + token + `")`
	if _, err := Compile(t.Context(), cfg, invalidSource, rootFile); err == nil {
		t.Fatal("included Secret pipeline accepted a plain source string")
	} else if strings.Contains(err.Error(), token) {
		t.Fatalf("compile diagnostic echoed source Secret: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := RunSource(t.Context(), cfg, include+"secrets.pipeline.handle\n", rootFile, map[string]any{"api_key": token}, Opts{
		Verbosity: 3,
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if code != 0 {
		t.Fatalf("RunSource exit code = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
		t.Fatalf("included pipeline trace leaked request Secret: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), secret.Redacted) {
		t.Fatalf("included pipeline final result omitted the redaction marker: stdout=%s", stdout.String())
	}
}

func TestSecretBearingActionsCannotBeDurablyJournaled(t *testing.T) {
	const token = "journal-secret-canary"
	var calls int
	consume := action.New("test.consume_secret", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		calls++
		return map[string]any{"echo": token}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "secret-journal-rejection-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{consume}}},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"test.consume_secret": {{Name: "api_key", Kind: compiler.ArgSecret}},
		},
	}
	cfg, err := BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	journalDir := t.TempDir()
	journal, err := NewFileJournal(journalDir)
	if err != nil {
		t.Fatalf("NewFileJournal: %v", err)
	}
	defer func() {
		if err := journal.Close(); err != nil {
			t.Errorf("close journal: %v", err)
		}
	}()
	cfg.Journal = journal
	source := `@schema Request {
  APIKey secret ` + "`json:\"api_key\"`" + `
}
@pipeline handle
  :request-schema=Request
  test.consume_secret(api_key: .api_key)
@end
pipeline.handle
`
	var stdout, stderr bytes.Buffer
	code := RunSource(t.Context(), cfg, source, "secret-journal.nflow", map[string]any{"api_key": token}, Opts{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code == 0 {
		t.Fatal("Secret-bearing action unexpectedly ran with durable journaling enabled")
	}
	if !strings.Contains(stderr.String(), "cannot be durably journaled") {
		t.Fatalf("journal rejection was not reported: %s", stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatalf("journal rejection diagnostic leaked the input Secret: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if calls != 0 {
		t.Fatalf("Secret action ran %d times before the journal rejection", calls)
	}
	steps, err := os.ReadDir(filepath.Join(journalDir, "steps"))
	if err != nil {
		t.Fatalf("read step journal: %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("journal contains %d step records after rejection", len(steps))
	}
}

func TestFinalOutputSanitizerCapturesOnlyDeclaredSecretFields(t *testing.T) {
	const token = "raw-response-secret-canary"
	type response struct {
		APIKey secret.Value `json:"api_key"`
	}
	scrubber := secret.NewScrubber()
	scrubber.Capture(map[string]any{"api_key": token, "unrelated": "ordinary payload"}, reflect.TypeFor[response]())
	ctx := secretScrubberContextKey.With(context.Background(), scrubber)
	got := SanitizeOutputText(ctx, "server echoed "+token)
	if strings.Contains(got, token) || !strings.Contains(got, secret.Redacted) {
		t.Fatalf("final output = %q, want captured Secret redacted", got)
	}
}
