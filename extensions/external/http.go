package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// HTTPResult carries the status and the decoded body. JSON responses
// are decoded into any; other bodies are returned as a string.
type HTTPResult struct {
	Status int `json:"status"`
	Body   any `json:"body"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// HTTPRequest performs an HTTP request and maps the status to a
// classified error kind (429 → TooManyRequests, 5xx → Unavailable, …).
// The response body is always returned, even on error, so a fallback
// pipeline can inspect it.
var HTTPRequest = action.New("http.request", func(ctx context.Context, in map[string]any) (HTTPResult, error) {
	reqURL := readStringField(in, "url")
	if reqURL == "" {
		return HTTPResult{}, xerr.BadRequest("http.request: 'url' parameter is required")
	}

	method := strings.ToUpper(readStringField(in, "method"))
	if method == "" {
		method = http.MethodGet
	}

	body, err := buildBody(in["body"])
	if err != nil {
		return HTTPResult{}, xerr.BadRequest("http.request: invalid body: " + err.Error())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return HTTPResult{}, xerr.Internal("http.request: build request", err)
	}

	if headers, ok := in["headers"].(map[string]any); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprint(v))
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return HTTPResult{}, xerr.Unavailable(fmt.Sprintf("http.request: %s %s failed", method, reqURL), err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return HTTPResult{}, xerr.Internal("http.request: read body", err)
	}

	result := HTTPResult{Status: resp.StatusCode, Body: decodeBody(raw)}

	if resp.StatusCode >= 400 {
		return result, errorFromStatus(resp.StatusCode, method, reqURL)
	}
	return result, nil
}).
	Description("Send HTTP request and decode result").
	Tag("external", "http", "network").
	Build()

func buildBody(raw any) (io.Reader, error) {
	if raw == nil {
		return http.NoBody, nil
	}
	switch v := raw.(type) {
	case string:
		return strings.NewReader(v), nil
	case []byte:
		return bytes.NewReader(v), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}
}

// decodeBody returns the parsed JSON body, or the raw string when the
// body is not JSON.
func decodeBody(raw []byte) any {
	var parsed any
	if json.Unmarshal(raw, &parsed) == nil {
		return parsed
	}
	return string(raw)
}

// errorFromStatus maps an HTTP status to the matching xerr kind so
// retry and circuit-breaker policies can react correctly.
func errorFromStatus(status int, method, url string) error {
	msg := fmt.Sprintf("http.request: %s %s returned %d", method, url, status)
	switch status {
	case http.StatusBadRequest:
		return xerr.BadRequest(msg)
	case http.StatusUnauthorized:
		return xerr.Unauthorized(msg)
	case http.StatusForbidden:
		return xerr.Forbidden(msg)
	case http.StatusNotFound:
		return xerr.NotFound(msg)
	case http.StatusMethodNotAllowed:
		return xerr.MethodNotAllowed(msg)
	case http.StatusConflict:
		return xerr.Conflict(msg)
	case http.StatusTooManyRequests:
		return xerr.TooManyRequests(msg)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return xerr.Timeout(msg)
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return xerr.Unavailable(msg)
	default:
		if status >= 500 {
			return xerr.Internal(msg)
		}
		return xerr.BadRequest(msg)
	}
}
