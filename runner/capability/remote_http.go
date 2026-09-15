package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// maxRemoteBody caps how many bytes we read from a remote capability. It
// protects the process from a misbehaving peer streaming gigabytes.
const maxRemoteBody = 16 << 20 // 16 MiB

// httpProxy builds an action that forwards the request body as JSON to the
// target URL via HTTP POST, and decodes the response body as JSON.
//
// Contract:
//
//	POST <target>
//	Content-Type: application/json
//	{"...": "request payload"}
//
//	200 OK
//	Content-Type: application/json
//	{"...": "response payload"}
//
// Non-2xx responses are wrapped as xerr.AppError so upstream retry and
// circuit-breaker middleware classify them correctly.
func httpProxy(b Binding) (action.AnyAction, error) {
	timeout := parseTimeout(b.Timeout, 30*time.Second)

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        32,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return action.New(b.Name, func(ctx context.Context, req any) (any, error) {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, xerr.Internal("marshal request failed", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.Target, bytes.NewReader(body))
		if err != nil {
			return nil, xerr.Internal("build request failed", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, xerr.Unavailable(fmt.Sprintf("remote %s unreachable", b.Target), err)
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteBody))
		if err != nil {
			return nil, xerr.Internal("read response failed", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, xerr.Unavailable(fmt.Sprintf("remote %s returned HTTP %d: %s",
				b.Target, resp.StatusCode, string(raw)))
		}

		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, xerr.Internal("decode response failed", err)
		}

		return out, nil
	}).
		Description("remote http capability: "+b.Target).
		Tag("capability", "remote", "http").
		Build(), nil
}
