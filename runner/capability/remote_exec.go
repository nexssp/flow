package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// execProxy builds an action that pipes the JSON request to a subprocess's
// stdin and decodes the subprocess's stdout as JSON.
//
// Contract:
//
//	stdin  : JSON-encoded request
//	stdout : JSON-encoded response
//	stderr : included in the error message on non-zero exit
//
// The command is run with `sh -c` on POSIX and `cmd /C` on Windows so that
// shell pipelines work on both platforms.
func execProxy(b Binding) (action.AnyAction, error) {
	if strings.TrimSpace(b.Target) == "" {
		return nil, fmt.Errorf("capability %s: empty exec command", b.Name)
	}

	timeout := parseTimeout(b.Timeout, 60*time.Second)

	return action.New(b.Name, func(ctx context.Context, req any) (any, error) {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, xerr.Internal("marshal request failed", err)
		}

		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := shellCommand(runCtx, b.Target)
		cmd.Stdin = bytes.NewReader(body)

		var stdout, stderr bytes.Buffer

		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return nil, xerr.Internal(fmt.Sprintf("exec %q failed: %v; stderr=%s",
				b.Target, err, stderr.String()))
		}

		var out any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			return nil, xerr.Internal(fmt.Sprintf("decode stdout failed: %v; stdout=%s",
				err, stdout.String()))
		}

		return out, nil
	}).
		Description("remote exec capability: "+b.Target).
		Tag("capability", "remote", "exec").
		Build(), nil
}
