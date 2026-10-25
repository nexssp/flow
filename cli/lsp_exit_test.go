package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLSPStdioExitWithoutShutdownReturnsFailure(t *testing.T) {
	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	//nolint:gosec // Launches this test binary only, with a fixed test filter, to exercise real stdio.
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLSPProtocolSubprocessHelper$")
	command.Env = append(os.Environ(), lspProtocolChildEnv+"=1")
	command.Stdin = bytes.NewReader(input.Bytes())
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("LSP exit without shutdown error = %v, want exit status 1; stderr: %s", err, stderr.String())
	}
	messages := readLSPTestMessages(t, &stdout)
	if len(messages) != 1 || string(messages[0].ID) != "1" || len(messages[0].Result) == 0 {
		t.Fatalf("LSP responses before unsuccessful exit = %#v, want initialize response", messages)
	}
}
