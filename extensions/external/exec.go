package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

var Exec = action.New("exec", func(ctx context.Context, in map[string]any) (ExecResult, error) {
	command := readStringField(in, "cmd", "command")
	if command == "" {
		return ExecResult{}, xerr.BadRequest("exec: 'cmd' parameter is required")
	}

	cmd := shellCommand(ctx, command)

	if input, ok := in["input"]; ok && input != nil {
		cmd.Stdin = bytes.NewReader(inputBytes(input))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	result := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
		OK:       runErr == nil,
		Output:   decodeJSON(stdout.String()),
	}

	if runErr == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if !asExitError(runErr, &exitErr) {
		return result, xerr.Internal("exec " + command + ": " + runErr.Error())
	}
	result.ExitCode = exitErr.ExitCode()

	return result, xerr.Internal(fmt.Sprintf(
		"exec %q failed with exit code %d: %s",
		command, result.ExitCode, strings.TrimSpace(result.Stderr)))
}).
	Description("Execute external shell/CLI command").
	Tag("external", "exec", "os").
	Build()

func inputBytes(input any) []byte {
	switch v := input.(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	default:
		data, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			return []byte(fmt.Sprint(v))
		}
		return data
	}
}

func decodeJSON(s string) any {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil
	}
	return out
}

func readStringField(in map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := in[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		*target = exitErr
		return true
	}
	return false
}
