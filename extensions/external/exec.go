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

// outputCaptureMaxBytes bounds each captured output stream independently.
// Writers continue accepting bytes after the cap so child processes keep draining.
const outputCaptureMaxBytes = 1 << 20

// outputCapture stores at most outputCaptureMaxBytes while reporting every byte
// as consumed, preventing a verbose child from blocking on a full pipe.
type outputCapture struct {
	buffer    bytes.Buffer
	truncated bool
}

func (c *outputCapture) Write(p []byte) (int, error) {
	remaining := outputCaptureMaxBytes - c.buffer.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = c.buffer.Write(p[:remaining])
	}
	if remaining < len(p) {
		c.truncated = true
	}
	return len(p), nil
}

// Exec runs cmd through the platform shell, or executes cmd directly with args
// when an argument array is supplied.
var Exec = action.New("external.exec", func(ctx context.Context, in map[string]any) (ExecResult, error) {
	command := readStringField(in, "cmd", "command")
	if command == "" {
		return ExecResult{}, xerr.BadRequest("exec: 'cmd' parameter is required")
	}

	var cmd *exec.Cmd
	if rawArgs, ok := in["args"]; ok {
		args, ok := readStringArgs(rawArgs)
		if !ok {
			return ExecResult{}, xerr.BadRequest("exec: 'args' parameter must be an array of strings")
		}
		cmd = exec.CommandContext(ctx, command, args...)
	} else {
		cmd = shellCommand(ctx, command)
	}

	if input, ok := in["input"]; ok && input != nil {
		cmd.Stdin = bytes.NewReader(inputBytes(input))
	}

	var stdout, stderr outputCapture
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	result := ExecResult{
		Stdout:   stdout.buffer.String(),
		Stderr:   stderr.buffer.String(),
		ExitCode: 0,
		OK:       runErr == nil,
		Output:   decodeJSON(stdout.buffer.String()),
	}

	if runErr == nil {
		if stdout.truncated || stderr.truncated {
			return result, outputTruncatedError("exec", stdout.truncated, stderr.truncated)
		}
		return result, nil
	}

	var exitErr *exec.ExitError
	isExitError := asExitError(runErr, &exitErr)
	if isExitError {
		result.ExitCode = exitErr.ExitCode()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if !isExitError {
		return result, xerr.Internal("exec " + command + ": " + runErr.Error())
	}

	truncation := ""
	if stdout.truncated || stderr.truncated {
		truncation = "; " + outputTruncatedError("exec", stdout.truncated, stderr.truncated).Error()
	}
	return result, xerr.Internal(fmt.Sprintf(
		"exec %q failed with exit code %d: %s%s",
		command, result.ExitCode, strings.TrimSpace(result.Stderr), truncation))
}).
	Description("Execute external shell/CLI command").
	Tag("external", "exec", "os").
	Build()

func outputTruncatedError(kind string, stdout, stderr bool) error {
	streams := make([]string, 0, 2)
	if stdout {
		streams = append(streams, "stdout")
	}
	if stderr {
		streams = append(streams, "stderr")
	}
	return xerr.Internal(fmt.Sprintf("%s output truncated: %s capped at %d bytes each", kind, strings.Join(streams, " and "), outputCaptureMaxBytes))
}

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

func readStringArgs(value any) ([]string, bool) {
	switch args := value.(type) {
	case []string:
		return args, true
	case []any:
		out := make([]string, len(args))
		for i, arg := range args {
			text, ok := arg.(string)
			if !ok {
				return nil, false
			}
			out[i] = text
		}
		return out, true
	default:
		return nil, false
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		*target = exitErr
		return true
	}
	return false
}
